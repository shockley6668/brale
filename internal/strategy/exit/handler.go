package exit

import (
	"context"

	"brale/internal/decision"
	"brale/internal/gateway/database"
)

// PlanHandler 负责解析/执行特定 exit plan。
type PlanHandler interface {
	// ID 返回唯一标识，需与 exit_strategies.yaml handler 字段一致。
	ID() string
	// Validate 校验 params 是否符合 handler 要求。
	Validate(params map[string]any) error
	// Instantiate 根据初始参数创建一个或多个 PlanInstance。
	Instantiate(ctx context.Context, args InstantiateArgs) ([]PlanInstance, error)
	// OnPrice 在价格事件触发时评估计划，必要时返回 PlanEvent。
	OnPrice(ctx context.Context, inst PlanInstance, price float64) (*PlanEvent, error)
	// OnAdjust 处理 AI 或人工对计划参数的调整。
	OnAdjust(ctx context.Context, inst PlanInstance, params map[string]any) (*PlanEvent, error)
}

// StrategyStore 定义策略实例的持久化接口，方便未来替换具体存储。
type StrategyStore interface {
	ListStrategyInstances(ctx context.Context, tradeID int) ([]database.StrategyInstanceRecord, error)
	UpdateStrategyInstanceState(ctx context.Context, tradeID int, planID, component, stateJSON string, status database.StrategyStatus) error
	InsertStrategyInstances(ctx context.Context, recs []database.StrategyInstanceRecord) error
	ActiveTradeIDs(ctx context.Context) ([]int, error)
	InsertStrategyChangeLog(ctx context.Context, rec database.StrategyChangeLogRecord) error
}

// InstantiateArgs 传递给 handler 的初始化参数。
type InstantiateArgs struct {
	TradeID       int
	PlanID        string
	PlanVersion   int
	PlanSpec      map[string]any
	Decision      decision.Decision
	EntryPrice    float64
	Side          string
	Symbol        string
	DecisionTrace string
}

// PlanInstance 表示 handler 运行期关心的实例快照。
type PlanInstance struct {
	Record database.StrategyInstanceRecord
	Plan   map[string]any
	State  map[string]any
}

// PlanEvent 用于统一 handler 输出，供上层决定是否下单/更新。
type PlanEvent struct {
	TradeID       int
	PlanID        string
	PlanComponent string
	Type          string
	Details       map[string]any
}

// 预定义的 PlanEvent.Type 文本，便于上下游识别。
const (
	PlanEventTypeTierHit         = "tier_hit"
	PlanEventTypeStopLoss        = "stop_loss"
	PlanEventTypeTakeProfit      = "take_profit"
	PlanEventTypeFinalStopLoss   = "final_stop_loss"
	PlanEventTypeFinalTakeProfit = "final_take_profit"
	PlanEventTypeAdjust          = "plan_adjust"
)
