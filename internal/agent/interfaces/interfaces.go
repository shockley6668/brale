package interfaces

import (
	"context"

	"brale/internal/decision"
	"brale/internal/gateway/exchange"
)

// PositionService handles account state and position tracking.
// Used by scheduler to build LLM context before each decision cycle.
type PositionService interface {
	// Returns balance, margin, and equity for LLM prompt context.
	GetAccountSnapshot(ctx context.Context) (decision.AccountSnapshot, error)

	// Fetches all open positions from exchange. Used for position-aware prompts.
	ListPositions(ctx context.Context) ([]decision.PositionSnapshot, error)

	// Loads persisted exit strategies and registers price watchers.
	// Hook is called when a strategy triggers (e.g., trailing stop hit).
	SyncStrategies(ctx context.Context, hook exchange.PlanUpdateHook) error

	// Maps symbol to freqtrade trade_id for webhook correlation.
	TradeIDForSymbol(symbol string) (int, bool)
}

// MarketService provides market data for LLM decision context.
// Aggregates klines, indicators, and real-time prices.
type MarketService interface {
	// Builds full analysis context per symbol: klines, indicators, OI, funding.
	GetAnalysisContexts(ctx context.Context, symbols []string) ([]decision.AnalysisContext, error)

	// Returns cached price or fetches from exchange. Used for decision validation.
	LatestPrice(ctx context.Context, symbol string) float64

	// Snapshots current indicator values for logging/debugging.
	CaptureIndicators(ctxs []decision.AnalysisContext)

	// Returns ATR for dynamic stop-loss calculation. Returns false if unavailable.
	GetATR(symbol string) (float64, bool)
}

// PlanAdjustSpec describes a strategy adjustment request.
// Source identifies who triggered the change (e.g., "trailing_stop", "manual").
type PlanAdjustSpec struct {
	TradeID   int
	PlanID    string
	Component string
	Params    map[string]any
	Source    string
}
