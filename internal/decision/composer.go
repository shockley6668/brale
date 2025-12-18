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

// Composer assembles the system and user prompts for the decision model.
type Composer struct {
	PromptMgr             *strategy.Manager
	Metrics               *market.MetricsService
	Intervals             []string // Timeframes to include in summary
	HorizonName           string
	DebugStructuredBlocks bool
}

// NewComposer creates a new prompt composer.
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

// Compose generating System and User prompts based on context.
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
	// TODO: insights integration if needed here or passed in input?
	// Legacy takes `insights` as separate arg to buildUserSummary.
	// We should probably rely on input.Analysis having the insights or input.Insights?
	// Legacy: `insights := e.runMultiAgents(ctx, input)` then `e.ComposePrompts(..., insights)`
	// The insights are rendered in specific section.
	// For StandardEngine, the "Agents" part might be computed *before* Compose?
	// In legacy, `runMultiAgents` modifies `input`? No, it returns `[]AgentInsight`.
	// For Phase 1 simplification, we assume `input` contains everything or we add `Insights` field to `Composer.Compose`?
	// `input.Analysis` contains `AgentInsight` inside `Context`? No.
	// `Context` struct has `Analysis []AnalysisContext`.
	// Legacy `runMultiAgents` logic seems to be separate.
	// For now, let's assume insights are passed via context or we refactor runMultiAgents later.
	// Wait, `Context` struct in `engine.go` DOES NOT have `Insights`.
	// But `buildUserSummary` in legacy uses `e.renderAgentBlocks(insights)`.
	// I will add `Insights` field to `Context` struct in `engine.go` later?
	// Or just accept it as arg? Decider interface takes `Context` only.
	// So `Context` MUST contain insights if we want Decider to be self-contained.
	// I will assume `Context` has `previous_reasoning` etc.

	return system, user, nil
}

// CollectImages extracts base64 images from analysis context for vision models.
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
	// Note: We need insights here.
	// In legacy, insights are computed before ComposePrompts.
	// If inputs.Analysis contains insights? No.
	// Let's check Context again.
	// Context has `Analysis []AnalysisContext`.
	// Context has `FeatureReports`.
	// We might need to extend Context to carry Insights if we want strict Decider interface.

	// Temporary mock for phase 1 porting
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

// --- Render Methods ported from legacy_adapter/render files ---

func (c *Composer) filterPositions(positions []types.PositionSnapshot, candidates []string) []types.PositionSnapshot {
	if len(positions) == 0 || len(candidates) == 0 {
		return nil
	}
	// Simple O(N*M) or map based. Since candidates are few (1-20), simple loop or map is fine.
	// We want case-insensitive matching.
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
