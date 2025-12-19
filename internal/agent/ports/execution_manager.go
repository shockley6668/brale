package ports

import (
	"context"

	"brale/internal/decision"
	"brale/internal/gateway/exchange"
)

// ExecutionManager bridges LLM decisions to actual exchange orders.
// Wraps freqtrade client, handles webhooks, and manages position lifecycle.
type ExecutionManager interface {
	// Bypasses LLM, directly opens a position via admin panel.
	ManualOpenPosition(context.Context, exchange.ManualOpenRequest) error

	// Balance queries - cached vs fresh from exchange.
	AccountBalance() exchange.Balance
	RefreshBalance(context.Context) (exchange.Balance, error)

	// Registers callback for exit strategy events (stop hit, tier filled).
	SetPlanUpdateHook(exchange.PlanUpdateHook)

	// Position state - used for decision context and strategy routing.
	ListOpenPositions(context.Context) ([]exchange.Position, error)
	TradeIDBySymbol(string) (int, bool)
	EntryPriceBySymbol(string) float64

	// Decision execution pipeline.
	// CacheDecision stores decision for webhook correlation.
	// Execute sends order to freqtrade via actor event loop.
	CacheDecision(string, decision.Decision) string
	Execute(context.Context, decision.DecisionInput) error

	// Strategy management - creates exit plan instances after entry fill.
	SyncStrategyPlans(context.Context, int, any) error
	CloseFreqtradePosition(context.Context, int, string, string, float64) error

	// Webhook from freqtrade (entry/exit/fill), routed to trader actor.
	HandleWebhook(context.Context, exchange.WebhookMessage)
	// API endpoints for admin dashboard.
	PositionsForAPI(context.Context, exchange.PositionListOptions) (exchange.PositionListResult, error)
	ListFreqtradeEvents(context.Context, int, int) ([]exchange.TradeEvent, error)
}
