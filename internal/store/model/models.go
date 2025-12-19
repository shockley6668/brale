package model

import (
	"time"

	"gorm.io/datatypes"
)

type LiveOrderStatus int

const (
	LiveOrderStatusUnknown        LiveOrderStatus = 0
	LiveOrderStatusOpen           LiveOrderStatus = 1
	LiveOrderStatusClosed         LiveOrderStatus = 2
	LiveOrderStatusPartial        LiveOrderStatus = 3
	LiveOrderStatusRetrying       LiveOrderStatus = 4
	LiveOrderStatusOpening        LiveOrderStatus = 5
	LiveOrderStatusClosingPartial LiveOrderStatus = 6
	LiveOrderStatusClosingFull    LiveOrderStatus = 7
	LiveOrderStatusCanceled       LiveOrderStatus = 8
)

type StrategyStatus int

const (
	StrategyStatusPending StrategyStatus = 0
	StrategyStatusActive  StrategyStatus = 1
	StrategyStatusDone    StrategyStatus = 2
	StrategyStatusFailed  StrategyStatus = 3
)

type LiveOrderModel struct {
	ID                int64           `gorm:"column:id;primaryKey"`
	FreqtradeID       int             `gorm:"column:freqtrade_id;uniqueIndex"`
	Symbol            string          `gorm:"column:symbol"`
	Side              string          `gorm:"column:side"`
	Amount            float64         `gorm:"column:amount"`
	InitialAmount     float64         `gorm:"column:initial_amount"`
	StakeAmount       float64         `gorm:"column:stake_amount"`
	Leverage          float64         `gorm:"column:leverage"`
	PositionValue     float64         `gorm:"column:position_value"`
	Price             float64         `gorm:"column:price"`
	ClosedAmount      float64         `gorm:"column:closed_amount"`
	PnLRatio          float64         `gorm:"column:pnl_ratio"`
	PnLUSD            float64         `gorm:"column:pnl_usd"`
	CurrentPrice      float64         `gorm:"column:current_price"`
	CurrentProfitRate float64         `gorm:"column:current_profit_ratio"`
	CurrentProfitAbs  float64         `gorm:"column:current_profit_abs"`
	UnrealizedRatio   float64         `gorm:"column:unrealized_pnl_ratio"`
	UnrealizedUSD     float64         `gorm:"column:unrealized_pnl_usd"`
	RealizedRatio     float64         `gorm:"column:realized_pnl_ratio"`
	RealizedUSD       float64         `gorm:"column:realized_pnl_usd"`
	IsSimulated       int             `gorm:"column:is_simulated"`
	Status            LiveOrderStatus `gorm:"column:status"`
	StartTimestamp    int64           `gorm:"column:start_timestamp"`
	EndTimestamp      int64           `gorm:"column:end_timestamp"`
	LastStatusSync    int64           `gorm:"column:last_status_sync"`
	RawData           string          `gorm:"column:raw_data"`
	CreatedAtUnix     int64           `gorm:"column:created_at"`
	UpdatedAtUnix     int64           `gorm:"column:updated_at"`

	CreatedAt time.Time `gorm:"-"`
	UpdatedAt time.Time `gorm:"-"`
}

func (LiveOrderModel) TableName() string { return "live_orders" }

type StrategyInstanceModel struct {
	ID              int64          `gorm:"column:id;primaryKey"`
	TradeID         int            `gorm:"column:trade_id;uniqueIndex:idx_strategy_instance,priority:1"`
	PlanID          string         `gorm:"column:plan_id;uniqueIndex:idx_strategy_instance,priority:2"`
	PlanComponent   string         `gorm:"column:plan_component;uniqueIndex:idx_strategy_instance,priority:3"`
	PlanVersion     int            `gorm:"column:plan_version"`
	ParamsJSON      datatypes.JSON `gorm:"column:params_json;type:TEXT"`
	StateJSON       datatypes.JSON `gorm:"column:state_json;type:TEXT"`
	Status          StrategyStatus `gorm:"column:status"`
	DecisionTraceID string         `gorm:"column:decision_trace_id"`
	LastEvalUnix    *int64         `gorm:"column:last_eval_at"`
	NextEvalUnix    *int64         `gorm:"column:next_eval_after"`
	CreatedAtUnix   int64          `gorm:"column:created_at"`
	UpdatedAtUnix   int64          `gorm:"column:updated_at"`

	CreatedAt time.Time `gorm:"-"`
	UpdatedAt time.Time `gorm:"-"`
}

func (StrategyInstanceModel) TableName() string { return "strategy_instances" }
