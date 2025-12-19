package position

import (
	"context"
	"strings"
	"time"

	"brale/internal/agent/interfaces"
	"brale/internal/agent/ports"
	"brale/internal/decision"
	"brale/internal/gateway/exchange"
	"brale/internal/logger"
)

type Service struct {
	manager ports.ExecutionManager
}

func NewService(manager ports.ExecutionManager) *Service {
	return &Service{
		manager: manager,
	}
}

var _ interfaces.PositionService = (*Service)(nil)

func (s *Service) GetAccountSnapshot(ctx context.Context) (decision.AccountSnapshot, error) {
	if s.manager == nil {
		return decision.AccountSnapshot{Currency: "USDT"}, nil
	}

	bal, err := s.manager.RefreshBalance(ctx)
	if err != nil {
		logger.Warnf("PositionService: refresh balance failed: %v", err)
		bal = s.manager.AccountBalance()
	}

	currency := bal.StakeCurrency
	if strings.TrimSpace(currency) == "" {
		currency = "USDT"
	}
	return decision.AccountSnapshot{
		Total:     bal.Total,
		Available: bal.Available,
		Used:      bal.Used,
		Currency:  currency,
		UpdatedAt: bal.UpdatedAt,
	}, nil
}

func (s *Service) ListPositions(ctx context.Context) ([]decision.PositionSnapshot, error) {
	if s.manager == nil {
		return nil, nil
	}

	positions, err := s.manager.ListOpenPositions(ctx)
	if err != nil {
		return nil, err
	}
	if len(positions) == 0 {
		return nil, nil
	}

	bal := s.manager.AccountBalance()
	total := bal.Total

	out := make([]decision.PositionSnapshot, 0, len(positions))
	now := time.Now().UnixMilli()

	for _, p := range positions {
		snap := decision.PositionSnapshot{
			Symbol:          p.Symbol,
			Side:            p.Side,
			Quantity:        p.Amount,
			EntryPrice:      p.EntryPrice,
			CurrentPrice:    p.CurrentPrice,
			UnrealizedPn:    p.UnrealizedPnL,
			UnrealizedPnPct: p.UnrealizedPnLRatio * 100,
			HoldingMs:       now - p.OpenedAt.UnixMilli(),
			Leverage:        p.Leverage,
			Stake:           p.StakeAmount,
		}

		stake := snap.Stake
		if stake <= 0 && snap.Quantity > 0 && snap.EntryPrice > 0 {
			leverage := snap.Leverage
			if leverage <= 0 {
				leverage = 1
			}
			stake = snap.Quantity * snap.EntryPrice / float64(leverage)
		}

		if total > 0 && stake > 0 {
			snap.AccountRatio = stake / total
		}
		out = append(out, snap)
	}
	return out, nil
}

func (s *Service) SyncStrategies(ctx context.Context, hook exchange.PlanUpdateHook) error {
	if s.manager == nil {
		return nil
	}
	s.manager.SetPlanUpdateHook(hook)

	return nil
}

func (s *Service) TradeIDForSymbol(symbol string) (int, bool) {
	if s.manager == nil {
		return 0, false
	}
	id, ok := s.manager.TradeIDBySymbol(symbol)
	return id, ok
}

func (s *Service) ExecuteDecision(ctx context.Context, traceID string, d decision.Decision, marketPrice float64) error {
	if s.manager == nil {
		return nil
	}

	input := decision.DecisionInput{
		TraceID:     traceID,
		Decision:    d,
		MarketPrice: marketPrice,
	}

	s.manager.CacheDecision(traceID, d)

	return s.manager.Execute(ctx, input)
}
