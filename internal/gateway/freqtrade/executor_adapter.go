package freqtrade

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"brale/internal/config"
	"brale/internal/gateway/exchange"
	"brale/internal/logger"
	symbolpkg "brale/internal/pkg/symbol"
)

type Adapter struct {
	client *Client
	cfg    *config.FreqtradeConfig
}

func NewAdapter(client *Client, cfg *config.FreqtradeConfig) *Adapter {
	return &Adapter{
		client: client,
		cfg:    cfg,
	}
}

func (a *Adapter) Name() string {
	return "freqtrade"
}

func (a *Adapter) OpenPosition(ctx context.Context, req exchange.OpenRequest) (*exchange.OpenResult, error) {

	payload := ForceEnterPayload{
		Pair:        a.toFreqtradePair(req.Symbol), // Use normalized symbol
		Side:        req.Side,
		StakeAmount: req.Amount,
		OrderType:   req.OrderType,
	}

	if req.Price > 0 {
		payload.Price = &req.Price
	}
	if req.Leverage > 0 {
		payload.Leverage = req.Leverage
	}

	logger.Infof("Adapter open position : %s %s %.2f", req.Symbol, req.Side, req.Amount)

	resp, err := a.client.ForceEnter(ctx, payload)
	if err != nil {
		return nil, fmt.Errorf("freqtrade forceenter failed: %w", err)
	}

	return &exchange.OpenResult{
		PositionID: strconv.Itoa(resp.TradeID),
	}, nil
}

func (a *Adapter) ClosePosition(ctx context.Context, req exchange.CloseRequest) error {
	// 1. Find active trade for symbol if ID is not provided
	// Ideally req.PositionID should be provided. But we support finding by symbol.

	tradeID := req.PositionID
	if tradeID == "" && req.Symbol != "" {
		trades, err := a.client.ListTrades(ctx)
		if err != nil {
			return fmt.Errorf("failed to list trades to find close target: %w", err)
		}
		for _, t := range trades {
			if strings.EqualFold(a.fromFreqtradePair(t.Pair), req.Symbol) && t.IsOpen { // Compare normalized symbols
				tradeID = strconv.Itoa(t.ID)
				break
			}
		}
	}

	if tradeID == "" {
		return fmt.Errorf("no active trade found for %s to close", req.Symbol)
	}

	payload := ForceExitPayload{
		TradeID: tradeID,
	}
	if req.Amount > 0 {
		payload.Amount = req.Amount
	}

	logger.Infof("Adapter ClosePosition: %s (TradeID: %s)", req.Symbol, tradeID)

	if err := a.client.ForceExit(ctx, payload); err != nil {
		return fmt.Errorf("freqtrade forceexit failed: %w", err)
	}

	return nil
}

func (a *Adapter) ListOpenPositions(ctx context.Context) ([]exchange.Position, error) {
	if a == nil || a.client == nil {
		return nil, fmt.Errorf("freqtrade adapter not initialized")
	}
	trades, err := a.client.ListTrades(ctx)
	if err != nil {
		return nil, err
	}
	positions := make([]exchange.Position, 0, len(trades))
	for _, tr := range trades {
		if p := a.tradeToExchangePosition(&tr); p != nil { // Use adapter method
			positions = append(positions, *p)
		}
	}
	return positions, nil
}

func (a *Adapter) GetPosition(ctx context.Context, positionID string) (*exchange.Position, error) {
	if a == nil || a.client == nil {
		return nil, fmt.Errorf("freqtrade adapter not initialized")
	}
	if positionID == "" {
		return nil, fmt.Errorf("positionID required")
	}
	id, err := strconv.Atoi(positionID)
	if err != nil {
		return nil, fmt.Errorf("invalid trade ID: %s", positionID)
	}

	tr, err := a.client.GetTrade(ctx, id)
	if err != nil {
		// handle not found
		return nil, err
	}
	if tr == nil {
		return nil, nil
	}
	pos := a.tradeToExchangePosition(tr) // Use adapter method
	return pos, nil
}

func (a *Adapter) GetBalance(ctx context.Context) (exchange.Balance, error) {
	if a == nil || a.client == nil {
		return exchange.Balance{}, fmt.Errorf("freqtrade adapter not initialized")
	}
	return a.client.GetBalance(ctx)
}

func (a *Adapter) GetPrice(ctx context.Context, symbol string) (exchange.PriceQuote, error) {
	return exchange.PriceQuote{}, fmt.Errorf("GetPrice not implemented for freqtrade")
}

// toFreqtradePair converts an internal symbol (e.g., "ETH/USDT") to a Freqtrade-specific pair (e.g., "ETH/USDT:USDT").
func (a *Adapter) toFreqtradePair(sym string) string {
	stakeCurrency := ""
	if a.cfg != nil {
		stakeCurrency = a.cfg.StakeCurrency
	}
	return symbolpkg.Freqtrade(stakeCurrency).ToExchange(sym)
}

// fromFreqtradePair converts a Freqtrade pair (e.g., "ETH/USDT:USDT") to an internal symbol (e.g., "ETH/USDT").
func (a *Adapter) fromFreqtradePair(ftPair string) string {
	stakeCurrency := ""
	if a.cfg != nil {
		stakeCurrency = a.cfg.StakeCurrency
	}
	return symbolpkg.Freqtrade(stakeCurrency).FromExchange(ftPair)
}

// tradeToExchangePosition helper converts Freqtrade trade to exchange.Position
func (a *Adapter) tradeToExchangePosition(t *Trade) *exchange.Position {
	if t == nil {
		return nil
	}
	side := "long"
	if t.IsShort || strings.Contains(strings.ToLower(t.Side), "short") {
		side = "short"
	}

	openedAt := parseTradeTime(t.OpenDate)

	return &exchange.Position{
		ID:          strconv.Itoa(t.ID),
		Symbol:      a.fromFreqtradePair(t.Pair), // Use normalized symbol
		Side:        side,
		Amount:      t.Amount,
		EntryPrice:  t.OpenRate,
		Leverage:    t.Leverage,
		StakeAmount: t.StakeAmount,
		OpenedAt:    openedAt,
		IsOpen:      t.IsOpen,

		UnrealizedPnL:      t.ProfitAbs,
		UnrealizedPnLRatio: t.ProfitRatio,
		RealizedPnL:        t.CloseProfitAbs,
		RealizedPnLRatio:   t.CloseProfit,
		CurrentPrice:       t.CurrentRate,
	}
}

func parseTradeTime(raw string) time.Time {
	layouts := []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05"}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t
		}
	}
	return time.Time{}
}
