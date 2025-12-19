package agent

import (
	"context"
	"fmt"
	"strings"

	"brale/internal/agent/interfaces"
	"brale/internal/gateway/exchange"
	"brale/internal/logger"
	livehttp "brale/internal/transport/http/live"
)

func (s *LiveService) HandleFreqtradeWebhook(ctx context.Context, msg exchange.WebhookMessage) error {
	if s == nil || s.execManager == nil {
		return fmt.Errorf("live service 未初始化")
	}
	logger.Infof("收到 freqtrade webhook: type=%s trade_id=%d pair=%s direction=%s",
		strings.ToLower(strings.TrimSpace(msg.Type)),
		int(msg.TradeID),
		strings.ToUpper(strings.TrimSpace(msg.Pair)),
		strings.ToLower(strings.TrimSpace(msg.Direction)))
	s.execManager.HandleWebhook(ctx, msg)
	return nil
}

func (s *LiveService) ListFreqtradePositions(ctx context.Context, opts exchange.PositionListOptions) (exchange.PositionListResult, error) {

	result := exchange.PositionListResult{
		Page:     opts.Page,
		PageSize: opts.PageSize,
	}
	if result.Page < 1 {
		result.Page = 1
	}
	if result.PageSize <= 0 {
		result.PageSize = 10
	}
	if result.PageSize > 500 {
		result.PageSize = 500
	}
	if s == nil || s.execManager == nil {
		return result, nil
	}
	return s.execManager.PositionsForAPI(ctx, opts)
}

func (s *LiveService) GetFreqtradePosition(ctx context.Context, tradeID int) (*exchange.APIPosition, error) {
	if s == nil || s.execManager == nil {
		return nil, fmt.Errorf("live service 未初始化")
	}
	type byID interface {
		APIPositionByID(context.Context, int) (*exchange.APIPosition, error)
	}
	if getter, ok := s.execManager.(byID); ok {
		return getter.APIPositionByID(ctx, tradeID)
	}

	opts := exchange.PositionListOptions{
		Page:        1,
		PageSize:    1000,
		Status:      "all",
		IncludeLogs: false,
	}
	res, err := s.execManager.PositionsForAPI(ctx, opts)
	if err != nil {
		return nil, err
	}
	for i := range res.Positions {
		if res.Positions[i].TradeID == tradeID {
			pos := res.Positions[i]
			return &pos, nil
		}
	}
	return nil, fmt.Errorf("position not found")
}

func (s *LiveService) RefreshFreqtradePosition(ctx context.Context, tradeID int) (*exchange.APIPosition, error) {
	if s == nil || s.execManager == nil {
		return nil, fmt.Errorf("live service 未初始化")
	}
	type refresher interface {
		RefreshAPIPosition(context.Context, int) (*exchange.APIPosition, error)
	}
	if r, ok := s.execManager.(refresher); ok {
		return r.RefreshAPIPosition(ctx, tradeID)
	}
	return s.GetFreqtradePosition(ctx, tradeID)
}

func (s *LiveService) GetLatestPriceQuote(ctx context.Context, symbol string) (exchange.PriceQuote, error) {
	if s == nil || s.monitor == nil {
		return exchange.PriceQuote{}, fmt.Errorf("price monitor 未启用")
	}
	return s.monitor.GetLatestPriceQuote(ctx, symbol)
}

func (s *LiveService) CloseFreqtradePosition(ctx context.Context, tradeID int, symbol, side string, closeRatio float64) error {
	if s == nil || s.execManager == nil {
		return fmt.Errorf("live service 未初始化")
	}

	return s.execManager.CloseFreqtradePosition(ctx, tradeID, symbol, side, closeRatio)
}

func (s *LiveService) ListFreqtradeEvents(ctx context.Context, tradeID int, limit int) ([]exchange.TradeEvent, error) {
	if s == nil || s.execManager == nil {
		return nil, fmt.Errorf("live service 未初始化")
	}
	return s.execManager.ListFreqtradeEvents(ctx, tradeID, limit)
}

func (s *LiveService) ManualOpenPosition(ctx context.Context, req exchange.ManualOpenRequest) error {
	if s == nil || s.execManager == nil {
		return fmt.Errorf("freqtrade 执行器未启用")
	}

	return s.execManager.ManualOpenPosition(ctx, req)
}

func (s *LiveService) AdjustPlan(ctx context.Context, req livehttp.PlanAdjustRequest) error {
	if s == nil || s.planScheduler == nil {
		return fmt.Errorf("plan scheduler 未初始化")
	}
	spec := interfaces.PlanAdjustSpec{
		TradeID:   req.TradeID,
		PlanID:    strings.TrimSpace(req.PlanID),
		Component: strings.TrimSpace(req.Component),
		Params:    req.Params,
		Source:    "Manual/Admin",
	}
	return s.planScheduler.AdjustPlan(ctx, spec)
}
