package engine

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"brale/internal/agent/interfaces"
	"brale/internal/agent/prompt"
	brcfg "brale/internal/config"
	"brale/internal/decision"
	"brale/internal/exitplan"
	"brale/internal/logger"
	"brale/internal/pkg/circuit"
	"brale/internal/profile"
	promptkit "brale/internal/prompt"
	"brale/internal/scheduler"
	"brale/internal/strategy/exit"

	"golang.org/x/sync/errgroup"
)

type LiveEngine struct {
	PosService     interfaces.PositionService
	MktService     interfaces.MarketService
	Decider        decision.Decider
	ProfileMgr     *profile.Manager
	Config         *brcfg.Config
	ExitPolicy     *ExitPlanPolicy
	CircuitBreaker *circuit.CircuitBreaker
	PlanScheduler  interface {
		AdjustPlan(ctx context.Context, spec interfaces.PlanAdjustSpec) error
	}

	ExitPlans       *exitplan.Registry
	ExitPlanPrompts map[string]promptkit.ExitPlanPrompt
	Notifier        Notifier
	PromptStrategy  *prompt.StandardStrategy
	Candidates      []string
}

type EngineParams struct {
	Config        *brcfg.Config
	PosService    interfaces.PositionService
	MktService    interfaces.MarketService
	Decider       decision.Decider
	ProfileMgr    *profile.Manager
	ExitPlans     *exitplan.Registry
	PlanHandlers  *exit.HandlerRegistry
	PlanScheduler interface {
		AdjustPlan(ctx context.Context, spec interfaces.PlanAdjustSpec) error
	}
	Candidates      []string
	ExitPlanPrompts map[string]promptkit.ExitPlanPrompt
	Notifier        Notifier
}

func NewLiveEngine(p EngineParams) *LiveEngine {
	policy := NewExitPlanPolicy(p.ExitPlans, p.PlanHandlers, p.ProfileMgr, p.MktService)
	cb := circuit.NewCircuitBreaker("LiveEngine", 5, 2*time.Minute)
	promptStrategy := prompt.NewStandardStrategy(p.ExitPlans, p.ExitPlanPrompts)

	return &LiveEngine{
		Config:          p.Config,
		PosService:      p.PosService,
		MktService:      p.MktService,
		Decider:         p.Decider,
		ProfileMgr:      p.ProfileMgr,
		ExitPolicy:      policy,
		CircuitBreaker:  cb,
		PlanScheduler:   p.PlanScheduler,
		Candidates:      p.Candidates,
		ExitPlans:       p.ExitPlans,
		ExitPlanPrompts: p.ExitPlanPrompts,
		Notifier:        p.Notifier,
		PromptStrategy:  promptStrategy,
	}
}

func (e *LiveEngine) Run(ctx context.Context) error {
	offset := 10 * time.Second
	runImmediately := brcfg.AIDecisionRunImmediately
	if e != nil && e.Config != nil {
		if e.Config.AI.DecisionOffsetSeconds > 0 {
			offset = time.Duration(e.Config.AI.DecisionOffsetSeconds) * time.Second
		}
	}

	symbols := e.resolveCandidates()
	if len(symbols) == 0 {
		logger.Warnf("LiveEngine: No candidates configured")
		<-ctx.Done()
		return ctx.Err()
	}

	logger.Infof("LiveEngine: Starting per-symbol aligned loops symbols=%d offset=%s run_immediately=%v", len(symbols), offset, runImmediately)

	group, gctx := errgroup.WithContext(ctx)
	for _, sym := range symbols {
		sym := sym
		group.Go(func() error {
			align, interval, multiple, ok := e.symbolSchedule(sym)
			if !ok {
				logger.Warnf("LiveEngine: skip symbol=%s: schedule unavailable", sym)
				<-gctx.Done()
				return gctx.Err()
			}
			cb := circuit.NewCircuitBreaker("LiveEngine."+sym, 5, 2*time.Minute)
			sched := scheduler.NewAlignedOnceScheduler(gctx, align, interval, offset)
			sched.Name = fmt.Sprintf("%s x%d", sym, multiple)
			sched.RunImmediately = runImmediately
			sched.Start(func() {
				if cb != nil && !cb.Allow() {
					logger.Warnf("LiveEngine: Circuit breaker open, skipping tick symbol=%s", sym)
					return
				}
				if err := e.tickSymbols(gctx, []string{sym}); err != nil {
					logger.Errorf("LiveEngine: Tick error symbol=%s err=%v", sym, err)
					if cb != nil {
						cb.RecordFailure()
					}
					return
				}
				if cb != nil {
					cb.RecordSuccess()
				}
			})
			return nil
		})
	}
	return group.Wait()
}

func (e *LiveEngine) resolveCandidates() []string {
	if e == nil {
		return nil
	}
	if len(e.Candidates) > 0 {
		out := make([]string, 0, len(e.Candidates))
		for _, sym := range e.Candidates {
			s := strings.ToUpper(strings.TrimSpace(sym))
			if s == "" {
				continue
			}
			out = append(out, s)
		}
		sort.Strings(out)
		return out
	}
	if e.ProfileMgr == nil {
		return nil
	}
	set := make(map[string]struct{})
	for _, rt := range e.ProfileMgr.Profiles() {
		if rt == nil {
			continue
		}
		for _, sym := range rt.Definition.TargetsUpper() {
			if sym == "" {
				continue
			}
			set[sym] = struct{}{}
		}
	}
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for sym := range set {
		out = append(out, sym)
	}
	sort.Strings(out)
	return out
}

func (e *LiveEngine) symbolSchedule(symbol string) (align time.Duration, interval time.Duration, multiple int, ok bool) {
	if e == nil || e.ProfileMgr == nil {
		return 0, 0, 0, false
	}
	rt, found := e.ProfileMgr.Resolve(symbol)
	if !found || rt == nil {
		return 0, 0, 0, false
	}
	min := time.Duration(0)
	for _, iv := range rt.Definition.IntervalsLower() {
		dur, ok := scheduler.ParseIntervalDuration(iv)
		if !ok || dur <= 0 {
			continue
		}
		if min == 0 || dur < min {
			min = dur
		}
	}
	if min <= 0 {
		return 0, 0, 0, false
	}
	multiple = rt.Definition.DecisionIntervalMultiple
	if multiple <= 0 {
		multiple = 1
	}
	align = min
	interval = align * time.Duration(multiple)
	if interval <= 0 {
		interval = align
		multiple = 1
	}
	return align, interval, multiple, true
}

func (e *LiveEngine) tickSymbols(ctx context.Context, candidates []string) error {

	if len(candidates) == 0 {
		return nil
	}

	start := time.Now()

	input, err := e.sense(ctx, candidates)
	if err != nil {
		return err
	}

	logger.Infof("AI Decision Loop Start candidates=%d symbols=%v positions=%d", len(input.Candidates), input.Candidates, len(input.Positions))

	res, err := e.Decider.Decide(ctx, input)
	if err != nil {
		return err
	}

	traceID := res.TraceID
	if traceID == "" {
		traceID = fmt.Sprintf("trace-%d", time.Now().UnixNano())
	}

	if len(res.Decisions) == 0 {
		logger.Infof("AI Decision Empty (Wait) trace=%s duration=%s", traceID, time.Since(start))
		return nil
	}

	prepared := e.prepareDecisions(res.Decisions, len(input.Positions) > 0)

	accepted := e.executeDecisions(ctx, prepared, traceID)

	e.notifyMetaSummary(res)

	logger.Infof("AI Decision Loop End trace=%s original=%d prepared=%d accepted=%d duration=%s",
		traceID, len(res.Decisions), len(prepared), len(accepted), time.Since(start))

	return nil
}

func (e *LiveEngine) RunCycle(ctx context.Context, symbols []string) error {
	if len(symbols) == 0 {
		return nil
	}

	input, err := e.sense(ctx, symbols)
	if err != nil {
		return fmt.Errorf("sense failed: %w", err)
	}
	result, err := e.Decider.Decide(ctx, input)
	if err != nil {
		return fmt.Errorf("decide failed: %w", err)
	}
	for _, d := range result.Decisions {
		if err := e.execute(ctx, result.TraceID, d); err != nil {
			logger.Errorf("Execute failed for %s: %v", d.Symbol, err)
		}
	}

	return nil
}

func (e *LiveEngine) prepareDecisions(items []decision.Decision, hasPositions bool) []decision.Decision {
	if len(items) == 0 {
		return nil
	}
	prepared := make([]decision.Decision, len(items))
	copy(prepared, items)
	for i := range prepared {
		prepared[i].Action = decision.NormalizeAction(prepared[i].Action)
	}
	prepared = decision.OrderAndDedup(prepared)

	if !hasPositions {
		filtered := prepared[:0]
		dropped := 0
		for _, d := range prepared {
			if d.Action == "close_long" || d.Action == "close_short" || d.Action == "update_exit_plan" {
				dropped++
				continue
			}
			filtered = append(filtered, d)
		}
		prepared = filtered
		if dropped > 0 {
			logger.Infof("Dropped %d decisions requiring positions", dropped)
		}
	}

	prepared = e.ExitPolicy.Apply(prepared)

	return prepared
}

func (e *LiveEngine) executeDecisions(ctx context.Context, decisions []decision.Decision, traceID string) []decision.Decision {
	if len(decisions) == 0 {
		return nil
	}
	accepted := make([]decision.Decision, 0, len(decisions))
	newOpens := 0

	for _, d := range decisions {
		e.applyTradingDefaults(&d)

		if err := decision.Validate(&d); err != nil {
			logger.Warnf("Decision invalid: %v | %+v", err, d)
			continue
		}

		if d.Action == "update_exit_plan" {
			if err := e.handleUpdateExitPlan(ctx, traceID, d); err != nil {
				logger.Warnf("Update plan failed: %v", err)
			} else {
				accepted = append(accepted, d)
			}
			continue
		}

		marketPrice := e.MktService.LatestPrice(ctx, d.Symbol)
		if marketPrice > 0 {
			if err := decision.ValidateWithPrice(&d, marketPrice, e.Config.Advanced.MinRiskReward); err != nil {
				logger.Warnf("Decision RR check failed: %v", err)
				continue
			}
		}

		if exec, ok := e.PosService.(interface {
			ExecuteDecision(ctx context.Context, traceID string, d decision.Decision, price float64) error
		}); ok {
			if err := exec.ExecuteDecision(ctx, traceID, d, marketPrice); err != nil {
				logger.Errorf("Execution failed for %s: %v", d.Symbol, err)
				continue
			}
		} else {
			logger.Warnf("PositionService does not support execution")
			continue
		}

		accepted = append(accepted, d)

		if e.Notifier != nil && e.PosService != nil {
			if d.Action == "open_long" || d.Action == "open_short" {
				e.notifyOpenAfterFill(ctx, d, marketPrice, "")
			}
		}

		if d.Action == "open_long" || d.Action == "open_short" {
			if newOpens >= e.Config.Advanced.MaxOpensPerCycle {
				logger.Infof("Max opens reached, skipping %s", d.Symbol)
				continue
			}
			newOpens++
		}
	}
	return accepted
}

func (e *LiveEngine) applyTradingDefaults(d *decision.Decision) {
	if d.Action != "open_long" && d.Action != "open_short" {
		return
	}
	if d.Leverage <= 0 {
		if def := e.Config.Trading.DefaultLeverage; def > 0 {
			d.Leverage = def
		}
	}
	if d.PositionSizeUSD <= 0 {
		if size := e.Config.Trading.PositionSizeUSD(); size > 0 {
			d.PositionSizeUSD = size
		}
	}
}

func (e *LiveEngine) handleUpdateExitPlan(ctx context.Context, traceID string, d decision.Decision) error {
	if e.PlanScheduler == nil {
		return fmt.Errorf("plan scheduler not available")
	}
	type updateProcessor interface {
		ProcessUpdateDecision(ctx context.Context, traceID string, d decision.Decision) error
	}
	if p, ok := e.PlanScheduler.(updateProcessor); ok {
		return p.ProcessUpdateDecision(ctx, traceID, d)
	}
	return fmt.Errorf("plan scheduler does not support process update decision")
}

func (e *LiveEngine) sense(ctx context.Context, symbols []string) (decision.Context, error) {
	acct, err := e.PosService.GetAccountSnapshot(ctx)
	if err != nil {
		logger.Warnf("GetAccountSnapshot failed: %v", err)

	}
	positions, err := e.PosService.ListPositions(ctx)
	if err != nil {
		logger.Warnf("ListPositions failed: %v", err)
	}
	analysis, err := e.MktService.GetAnalysisContexts(ctx, symbols)
	if err != nil {
		logger.Warnf("GetAnalysisContexts failed: %v", err)
	}
	e.MktService.CaptureIndicators(analysis)
	input := decision.Context{
		Candidates: symbols,
		Account:    acct,
		Positions:  positions,
		Analysis:   analysis,
	}
	input.Directives = e.buildProfileDirectives(symbols)
	if e.ProfileMgr != nil && e.PromptStrategy != nil {
		activeProfiles := make(map[string]*profile.Runtime)
		allProfiles := make([]*profile.Runtime, 0)
		for _, sym := range symbols {
			s := strings.ToUpper(strings.TrimSpace(sym))
			rt, ok := e.ProfileMgr.Resolve(s)
			if !ok || rt == nil {
				continue
			}
			activeProfiles[rt.Definition.Name] = rt
			allProfiles = append(allProfiles, rt)
		}
		if err := e.PromptStrategy.Build(&input, activeProfiles, nil, allProfiles); err != nil {
			logger.Warnf("PromptStrategy.Build failed: %v", err)
		}
	}

	return input, nil
}

func (e *LiveEngine) execute(ctx context.Context, traceID string, d decision.Decision) error {
	price := e.MktService.LatestPrice(ctx, d.Symbol)
	if exec, ok := e.PosService.(interface {
		ExecuteDecision(ctx context.Context, traceID string, d decision.Decision, price float64) error
	}); ok {
		return exec.ExecuteDecision(ctx, traceID, d, price)
	}

	return fmt.Errorf("PositionService does not support execution")
}

func (e *LiveEngine) buildProfileDirectives(symbols []string) map[string]decision.ProfileDirective {
	if e.ProfileMgr == nil {
		return nil
	}
	directives := make(map[string]decision.ProfileDirective)
	for _, sym := range symbols {
		s := strings.ToUpper(strings.TrimSpace(sym))
		rt, ok := e.ProfileMgr.Resolve(s)
		if !ok || rt == nil {
			continue
		}
		directives[s] = decision.ProfileDirective{
			DerivativesEnabled: rt.Derivatives.Enabled,
			IncludeOI:          rt.Derivatives.IncludeOI,
			IncludeFunding:     rt.Derivatives.IncludeFunding,
			MultiAgentEnabled:  rt.AgentEnabled,
		}
	}
	return directives
}
