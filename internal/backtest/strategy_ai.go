package backtest

import (
	"context"
	"fmt"
	"strings"

	"brale/internal/ai"
	"brale/internal/prompt"
	"brale/internal/store"
)

// AIProxyFactory 基于 LegacyEngineAdapter 的策略工厂。
type AIProxyFactory struct {
	Prompt         *prompt.Manager
	SystemTemplate string
	Models         []ai.ModelCfg
	Aggregator     ai.Aggregator
	Parallel       bool
	TimeoutSeconds int
}

func (f *AIProxyFactory) NewStrategy(spec StrategySpec) (Strategy, error) {
	if f == nil {
		return nil, fmt.Errorf("空策略工厂")
	}
	providers := ai.BuildProvidersFromConfig(f.Models)
	if len(providers) == 0 {
		return nil, fmt.Errorf("未启用任何模型 Provider")
	}
	agg := f.Aggregator
	if agg == nil {
		agg = ai.FirstWinsAggregator{}
	}
	strat := &aiProxyStrategy{}
	engine := &ai.LegacyEngineAdapter{
		Providers:      providers,
		Agg:            agg,
		PromptMgr:      f.Prompt,
		SystemTemplate: f.SystemTemplate,
		Intervals:      spec.Profile.AllTimeframes(),
		Horizon:        spec.Profile,
		HorizonName:    spec.ProfileName,
		Parallel:       f.Parallel,
		TimeoutSeconds: f.TimeoutSeconds,
		Observer:       strat,
	}
	strat.engine = engine
	return strat, nil
}

type aiProxyStrategy struct {
	engine   *ai.LegacyEngineAdapter
	lastLogs []AILogRecord
}

func (s *aiProxyStrategy) Decide(ctx context.Context, req StrategyRequest) (ai.DecisionResult, error) {
	if s.engine == nil {
		return ai.DecisionResult{}, fmt.Errorf("AI 引擎未初始化")
	}
	s.lastLogs = nil
	mem := store.NewMemoryKlineStore()
	for tf, candles := range req.Timeframes {
		if len(candles) == 0 {
			continue
		}
		kls := make([]store.Kline, 0, len(candles))
		for _, c := range candles {
			kls = append(kls, store.Kline{
				OpenTime:  c.OpenTime,
				CloseTime: c.CloseTime,
				Open:      c.Open,
				High:      c.High,
				Low:       c.Low,
				Close:     c.Close,
				Volume:    c.Volume,
			})
		}
		if err := mem.Put(ctx, strings.ToUpper(req.Symbol), tf, kls, len(kls)); err != nil {
			return ai.DecisionResult{}, err
		}
	}
	s.engine.KStore = mem
	s.engine.Horizon = req.Profile
	if req.ProfileName != "" {
		s.engine.HorizonName = req.ProfileName
	}
	if intervals := req.Profile.AllTimeframes(); len(intervals) > 0 {
		s.engine.Intervals = intervals
	}
	return s.engine.Decide(ctx, ai.Context{
		Candidates: []string{strings.ToUpper(req.Symbol)},
		Positions:  toSnapshots(req.Positions),
	})
}

func (s *aiProxyStrategy) Logs() []AILogRecord {
	out := make([]AILogRecord, len(s.lastLogs))
	copy(out, s.lastLogs)
	s.lastLogs = nil
	return out
}

func (s *aiProxyStrategy) Close() error { return nil }

// AfterDecide implements ai.DecisionObserver.
func (s *aiProxyStrategy) AfterDecide(ctx context.Context, trace ai.DecisionTrace) {
	logs := make([]AILogRecord, 0, len(trace.Outputs)+1)
	for _, out := range trace.Outputs {
		rec := AILogRecord{
			ProviderID:   out.ProviderID,
			Stage:        "provider",
			SystemPrompt: trace.SystemPrompt,
			UserPrompt:   trace.UserPrompt,
			RawOutput:    out.Raw,
			RawJSON:      out.Parsed.RawJSON,
			Decisions:    append([]ai.Decision(nil), out.Parsed.Decisions...),
			Note:         "provider",
		}
		if out.Err != nil {
			rec.Error = out.Err.Error()
		}
		logs = append(logs, rec)
	}
	finalID := trace.Best.ProviderID
	if finalID == "" {
		finalID = "aggregate"
	}
	finalRec := AILogRecord{
		ProviderID:   finalID,
		Stage:        "final",
		SystemPrompt: trace.SystemPrompt,
		UserPrompt:   trace.UserPrompt,
		RawOutput:    trace.Best.Raw,
		RawJSON:      trace.Best.Parsed.RawJSON,
		MetaSummary:  trace.Best.Parsed.MetaSummary,
		Decisions:    append([]ai.Decision(nil), trace.Best.Parsed.Decisions...),
		Note:         "final",
	}
	if trace.Best.Err != nil {
		finalRec.Error = trace.Best.Err.Error()
	}
	logs = append(logs, finalRec)
	s.lastLogs = logs
}

func toSnapshots(src []StrategyPosition) []ai.PositionSnapshot {
	if len(src) == 0 {
		return nil
	}
	out := make([]ai.PositionSnapshot, 0, len(src))
	for _, p := range src {
		out = append(out, ai.PositionSnapshot{
			Symbol:          p.Symbol,
			Side:            p.Side,
			EntryPrice:      p.EntryPrice,
			Quantity:        p.Quantity,
			TakeProfit:      p.TakeProfit,
			StopLoss:        p.StopLoss,
			UnrealizedPn:    p.UnrealizedPn,
			UnrealizedPnPct: p.UnrealizedPnPct,
			RR:              p.RR,
			HoldingMs:       p.HoldingMs,
		})
	}
	return out
}
