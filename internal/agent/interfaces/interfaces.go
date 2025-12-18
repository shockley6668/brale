package interfaces

import (
	"context"

	"brale/internal/decision"
	"brale/internal/gateway/exchange"
)

// PositionService manages account balance and open positions.
type PositionService interface {
	// GetAccountSnapshot returns the latest account balance and status.
	GetAccountSnapshot(ctx context.Context) (decision.AccountSnapshot, error)

	// ListPositions returns all currently open positions.
	ListPositions(ctx context.Context) ([]decision.PositionSnapshot, error)

	// SyncStrategies synchronizes local strategy plans with the exchange/executor.
	SyncStrategies(ctx context.Context, hook exchange.PlanUpdateHook) error

	// TradeIDForSymbol returns the trade ID for a given symbol if it exists.
	TradeIDForSymbol(symbol string) (int, bool)
}

// MarketService manages market data (K-lines, prices, indicators).
type MarketService interface {
	// GetAnalysisContexts fetches market analysis data for the given symbols.
	GetAnalysisContexts(ctx context.Context, symbols []string) ([]decision.AnalysisContext, error)

	// LatestPrice returns the most recent price for a symbol.
	LatestPrice(ctx context.Context, symbol string) float64

	// CaptureIndicators updates internal cache of indicators (e.g. ATR) from analysis contexts.
	CaptureIndicators(ctxs []decision.AnalysisContext)

	// GetATR returns cached ATR for a symbol.
	GetATR(symbol string) (float64, bool)
}

// PlanAdjustSpec describes parameters for adjusting a plan.
type PlanAdjustSpec struct {
	TradeID   int
	PlanID    string
	Component string
	Params    map[string]any
	Source    string
}
