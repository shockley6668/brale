package decision

import (
	"context"
	"fmt"
	"strings"
	"time"

	"brale/internal/decision/render"
	"brale/internal/gateway/provider"
	"brale/internal/market"
	"brale/internal/strategy"
	"brale/internal/types"

	"golang.org/x/sync/errgroup"
)

type Composer struct {
	PromptMgr             *strategy.Manager
	Metrics               *market.MetricsService
	Intervals             []string
	HorizonName           string
	DebugStructuredBlocks bool
}

func NewComposer(mgr *strategy.Manager, metrics *market.MetricsService) *Composer {
	return &Composer{
		PromptMgr: mgr,
		Metrics:   metrics,
	}
}

func (c *Composer) SetIntervals(intervals []string) {
	c.Intervals = make([]string, len(intervals))
	copy(c.Intervals, intervals)
}

func (c *Composer) SetHorizonName(name string) {
	c.HorizonName = name
}

func (c *Composer) Compose(ctx context.Context, input Context) (string, string, error) {
	system := strings.TrimSpace(input.Prompt.System)
	userSummary := c.buildUserSummary(ctx, input)
	userExtra := strings.TrimSpace(input.Prompt.User)

	var user string
	switch {
	case userSummary != "" && userExtra != "":
		user = userSummary + "\n\n" + userExtra
	case userSummary != "":
		user = userSummary
	default:
		user = userExtra
	}

	return system, user, nil
}

func (c *Composer) CollectImages(ctxs []AnalysisContext) []provider.ImagePayload {
	if len(ctxs) == 0 {
		return nil
	}
	out := make([]provider.ImagePayload, 0, 4)
	for _, ac := range ctxs {
		if ac.ImageB64 == "" {
			continue
		}
		desc := fmt.Sprintf("%s %s %s", ac.Symbol, ac.Interval, ac.ImageNote)
		out = append(out, provider.ImagePayload{
			DataURI:     ac.ImageB64,
			Description: strings.TrimSpace(desc),
		})
		if len(out) >= cap(out) {
			break
		}
	}
	return out
}

func (c *Composer) buildUserSummary(ctx context.Context, input Context) string {

	var insights []AgentInsight

	c.refreshDerivativesOnDemand(ctx, input.Candidates, input.Directives)
	sections := render.Sections{
		Account:     c.renderAccountOverview(input.Account),
		Previous:    c.renderPreviousReasoning(input.PreviousReasoning),
		Derivatives: c.renderDerivativesMetrics(input.Candidates, input.Directives),
		Positions:   c.renderPositionDetails(c.filterPositions(input.Positions, input.Candidates)),
		Klines:      c.renderKlineWindows(input.Analysis),
		Agents:      c.renderAgentBlocks(insights),
		Guidelines:  c.renderOutputConstraints(input),
	}

	var loader render.TemplateLoader
	if c.PromptMgr != nil {
		loader = c.PromptMgr
	}
	summary := render.RenderSummary(loader, sections)
	c.logStructuredBlocksDebug(input.Analysis)
	return summary
}

func (c *Composer) refreshDerivativesOnDemand(ctx context.Context, symbols []string, directives map[string]ProfileDirective) {
	if c == nil || c.Metrics == nil || len(symbols) == 0 || len(directives) == 0 {
		return
	}
	need := make([]string, 0, len(symbols))
	for _, sym := range symbols {
		dir, ok := lookupDirective(sym, directives)
		if !ok || !dir.allowDerivatives() {
			continue
		}
		s := strings.ToUpper(strings.TrimSpace(sym))
		if s == "" {
			continue
		}
		need = append(need, s)
	}
	if len(need) == 0 {
		return
	}
	var eg errgroup.Group
	eg.SetLimit(4)
	for _, sym := range need {
		sym := sym
		eg.Go(func() error {
			refreshCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			c.Metrics.RefreshSymbol(refreshCtx, sym)
			return nil
		})
	}
	_ = eg.Wait()
}

func (c *Composer) filterPositions(positions []types.PositionSnapshot, candidates []string) []types.PositionSnapshot {
	if len(positions) == 0 || len(candidates) == 0 {
		return nil
	}

	candidateMap := make(map[string]struct{}, len(candidates))
	for _, sym := range candidates {
		s := strings.ToUpper(strings.TrimSpace(sym))
		if s != "" {
			candidateMap[s] = struct{}{}
		}
	}
	filtered := make([]types.PositionSnapshot, 0)
	for _, pos := range positions {
		s := strings.ToUpper(strings.TrimSpace(pos.Symbol))
		if _, ok := candidateMap[s]; ok {
			filtered = append(filtered, pos)
		}
	}
	return filtered
}
