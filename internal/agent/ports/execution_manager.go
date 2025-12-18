package ports

import (
	"context"

	"brale/internal/decision"
	"brale/internal/gateway/exchange"
)

// ExecutionManager is the minimal execution port required by the agent layer.
// It intentionally lives in the consumer side (agent) to avoid pushing business
// dependencies (e.g. decision) down into gateway/exchange.
type ExecutionManager interface {
	ManualOpenPosition(context.Context, exchange.ManualOpenRequest) error

	AccountBalance() exchange.Balance
	RefreshBalance(context.Context) (exchange.Balance, error)

	SetPlanUpdateHook(exchange.PlanUpdateHook)

	ListOpenPositions(context.Context) ([]exchange.Position, error)
	TradeIDBySymbol(string) (int, bool)
	EntryPriceBySymbol(string) float64

	CacheDecision(string, decision.Decision) string
	Execute(context.Context, decision.DecisionInput) error

	SyncStrategyPlans(context.Context, int, any) error
	CloseFreqtradePosition(context.Context, int, string, string, float64) error

	HandleWebhook(context.Context, exchange.WebhookMessage)
	PositionsForAPI(context.Context, exchange.PositionListOptions) (exchange.PositionListResult, error)
	ListFreqtradeEvents(context.Context, int, int) ([]exchange.TradeEvent, error)
}
