package agent

import (
	"brale/internal/pkg/circuit"
	"context"
	"fmt"
	"strings"
	"time"

	"brale/internal/agent/engine"
	"brale/internal/agent/ports"
	mktsvc "brale/internal/agent/service/market"
	"brale/internal/agent/service/position"
	brcfg "brale/internal/config"
	"brale/internal/decision"
	"brale/internal/exitplan"
	"brale/internal/gateway/database"
	"brale/internal/gateway/notifier"
	"brale/internal/market"
	"brale/internal/profile"
	promptkit "brale/internal/prompt"
	"brale/internal/strategy/exit"

	"golang.org/x/sync/errgroup"
)

type LiveServiceParams struct {
	Config          *brcfg.Config
	KlineStore      market.KlineStore
	Updater         *market.WSUpdater
	Metrics         *market.MetricsService
	Engine          decision.Decider
	Telegram        *notifier.Telegram
	DecisionLogs    *database.DecisionLogStore
	Symbols         []string
	Intervals       []string
	HorizonName     string
	HorizonSummary  string
	WarmupSummary   string
	ExecManager     ports.ExecutionManager
	VisionReady     bool
	ProfileManager  *profile.Manager
	ExitPlans       *exitplan.Registry
	PlanHandlers    *exit.HandlerRegistry
	StrategyStore   exit.StrategyStore
	ExitPlanPrompts map[string]promptkit.ExitPlanPrompt
}

type LiveService struct {
	cfg        *brcfg.Config
	monitor    *PriceMonitor
	liveEngine *engine.LiveEngine
	tg         *notifier.Telegram
	decLogs    *database.DecisionLogStore

	symbols       []string
	hIntervals    []string
	horizonName   string
	hSummary      string
	warmupSummary string

	execManager ports.ExecutionManager

	profileMgr     *profile.Manager
	exitPlans      *exitplan.Registry
	planHandlers   *exit.HandlerRegistry
	planScheduler  *PlanScheduler
	strategyStore  exit.StrategyStore
	strategyCloser interface {
		Close() error
	}

	circuitBreaker *circuit.CircuitBreaker

	metrics *market.MetricsService
}

func NewLiveService(p LiveServiceParams) *LiveService {
	var textNotifier notifier.TextNotifier
	var structuredNotifier engine.Notifier
	if p.Telegram != nil {
		textNotifier = p.Telegram
		structuredNotifier = p.Telegram
	}

	var planScheduler *PlanScheduler
	if p.StrategyStore != nil && p.PlanHandlers != nil && p.ExitPlans != nil {
		planScheduler = NewPlanScheduler(PlanSchedulerParams{
			Store:       p.StrategyStore,
			Plans:       p.ExitPlans,
			Handlers:    p.PlanHandlers,
			ExecManager: p.ExecManager,
			Notifier:    textNotifier,
		})
	}

	var monitor *PriceMonitor
	var symbols []string
	if len(p.Symbols) > 0 {
		symbols = append([]string(nil), p.Symbols...)
	}
	var intervals []string
	if len(p.Intervals) > 0 {
		intervals = append([]string(nil), p.Intervals...)
	}

	if p.Updater != nil || p.KlineStore != nil {
		monitor = NewPriceMonitor(MonitorParams{
			Updater:        p.Updater,
			KlineStore:     p.KlineStore,
			Symbols:        symbols,
			Intervals:      intervals,
			HorizonSummary: p.HorizonSummary,
			WarmupSummary:  p.WarmupSummary,
			Telegram:       p.Telegram,
			ExecManager:    p.ExecManager,
			Observer:       planScheduler,
		})
	}

	posSvc := position.NewService(p.ExecManager)

	mktParams := mktsvc.ServiceParams{
		Config:      p.Config,
		KlineStore:  p.KlineStore,
		ProfileMgr:  p.ProfileManager,
		Monitor:     monitor,
		Intervals:   intervals,
		HorizonName: p.HorizonName,
		VisionReady: p.VisionReady,
	}
	mktSvc := mktsvc.NewService(mktParams)

	engParams := engine.EngineParams{
		Config:          p.Config,
		PosService:      posSvc,
		MktService:      mktSvc,
		Decider:         p.Engine,
		ProfileMgr:      p.ProfileManager,
		Candidates:      p.Symbols,
		ExitPlans:       p.ExitPlans,
		PlanHandlers:    p.PlanHandlers,
		PlanScheduler:   planScheduler,
		ExitPlanPrompts: p.ExitPlanPrompts,
		Notifier:        structuredNotifier,
	}
	liveEngine := engine.NewLiveEngine(engParams)

	svc := &LiveService{
		cfg:            p.Config,
		liveEngine:     liveEngine,
		tg:             p.Telegram,
		decLogs:        p.DecisionLogs,
		metrics:        p.Metrics,
		horizonName:    p.HorizonName,
		hSummary:       p.HorizonSummary,
		warmupSummary:  p.WarmupSummary,
		execManager:    p.ExecManager,
		profileMgr:     p.ProfileManager,
		exitPlans:      p.ExitPlans,
		planHandlers:   p.PlanHandlers,
		strategyStore:  p.StrategyStore,
		circuitBreaker: circuit.NewCircuitBreaker("LiveService", 5, 2*time.Minute),
		symbols:        symbols,
		hIntervals:     intervals,
		planScheduler:  planScheduler,
		monitor:        monitor,
	}

	if planStore := p.StrategyStore; planStore != nil {
		if closable, ok := planStore.(interface{ Close() error }); ok {
			svc.strategyCloser = closable
		}
	}
	if svc.planScheduler != nil && svc.execManager != nil {
		svc.execManager.SetPlanUpdateHook(svc.planScheduler)

	}
	return svc
}

func (s *LiveService) Run(ctx context.Context) error {
	if s.metrics != nil {
		go s.metrics.Start(ctx)
	}
	s.prewarmDerivatives(ctx)
	if s.planScheduler != nil {
		s.planScheduler.Start(ctx)
	}
	if s.monitor != nil {
		s.monitor.Start(ctx)
	}

	if s.liveEngine != nil {
		return s.liveEngine.Run(ctx)
	}

	return fmt.Errorf("live engine not initialized")
}

func (s *LiveService) prewarmDerivatives(ctx context.Context) {
	if s == nil {
		return
	}
	base := ctx
	if base == nil {
		base = context.Background()
	}
	if s.metrics != nil && len(s.symbols) > 0 {
		go func() {
			var eg errgroup.Group
			eg.SetLimit(4)
			for _, sym := range s.symbols {
				sym := strings.TrimSpace(sym)
				if sym == "" {
					continue
				}
				eg.Go(func() error {
					refreshCtx, cancel := context.WithTimeout(base, 5*time.Second)
					defer cancel()
					s.metrics.RefreshSymbol(refreshCtx, sym)
					return nil
				})
			}
			_ = eg.Wait()
		}()
	}
	if fg := s.lookupFearGreedService(); fg != nil {
		go fg.RefreshIfStale(base)
	}
}

func (s *LiveService) lookupFearGreedService() *market.FearGreedService {
	if s == nil || s.liveEngine == nil || s.liveEngine.Decider == nil {
		return nil
	}
	eng, ok := s.liveEngine.Decider.(*decision.DecisionEngine)
	if !ok || eng.PromptBuilder == nil {
		return nil
	}
	pb, ok := eng.PromptBuilder.(*decision.DefaultPromptBuilder)
	if !ok {
		return nil
	}
	return pb.FearGreed
}
