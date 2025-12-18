package database

import (
	"context"
	"time"

	storemodel "brale/internal/store/model"
)

// LivePositionStore 描述仓位/tiers 的持久化能力。
type LivePositionStore interface {
	UpsertLiveOrder(ctx context.Context, rec LiveOrderRecord) error
	UpdateOrderStatus(ctx context.Context, tradeID int, status LiveOrderStatus) error
	// UpsertLiveTiers removed: In-Memory Projection pattern used instead.
	SavePosition(ctx context.Context, order LiveOrderRecord) error // Removed tier arg
	InsertStrategyInstances(ctx context.Context, recs []StrategyInstanceRecord) error
	UpdateStrategyInstanceState(ctx context.Context, tradeID int, planID, planComponent, stateJSON string, status StrategyStatus) error
	ListStrategyInstances(ctx context.Context, tradeID int) ([]StrategyInstanceRecord, error)
	AppendTradeOperation(ctx context.Context, op TradeOperationRecord) error
	ListTradeOperations(ctx context.Context, freqtradeID int, limit int) ([]TradeOperationRecord, error)
	GetLivePosition(ctx context.Context, freqtradeID int) (LiveOrderRecord, bool, error) // Removed tier return
	ListActivePositions(ctx context.Context, limit int) ([]LiveOrderRecord, error)       // Changed return type
	ListRecentPositions(ctx context.Context, limit int) ([]LiveOrderRecord, error)       // Changed return type
	ListRecentPositionsPaged(ctx context.Context, symbol string, limit int, offset int) ([]LiveOrderRecord, error)
	CountRecentPositions(ctx context.Context, symbol string) (int, error)
	AddOrderPnLColumns() error
	FinalizeStrategies(ctx context.Context, tradeID int) error
	FinalizePendingStrategies(ctx context.Context, tradeID int) error

	// Event Sourcing
	AppendEvent(ctx context.Context, evt EventRecord) error
	LoadEvents(ctx context.Context, since time.Time, limit int) ([]EventRecord, error)
}

// LiveOrderStatus 对应 live_orders.status。
type LiveOrderStatus = storemodel.LiveOrderStatus

const (
	LiveOrderStatusUnknown        LiveOrderStatus = storemodel.LiveOrderStatusUnknown
	LiveOrderStatusOpen           LiveOrderStatus = storemodel.LiveOrderStatusOpen
	LiveOrderStatusClosed         LiveOrderStatus = storemodel.LiveOrderStatusClosed
	LiveOrderStatusPartial        LiveOrderStatus = storemodel.LiveOrderStatusPartial
	LiveOrderStatusRetrying       LiveOrderStatus = storemodel.LiveOrderStatusRetrying
	LiveOrderStatusOpening        LiveOrderStatus = storemodel.LiveOrderStatusOpening
	LiveOrderStatusClosingPartial LiveOrderStatus = storemodel.LiveOrderStatusClosingPartial
	LiveOrderStatusClosingFull    LiveOrderStatus = storemodel.LiveOrderStatusClosingFull
	LiveOrderStatusCanceled       LiveOrderStatus = storemodel.LiveOrderStatusCanceled
)

// LiveOrderRecord 表示 live_orders 数据。
type LiveOrderRecord struct {
	FreqtradeID        int
	Symbol             string
	Side               string
	Amount             *float64
	InitialAmount      *float64
	StakeAmount        *float64
	Leverage           *float64
	PositionValue      *float64
	Price              *float64
	ClosedAmount       *float64
	IsSimulated        *bool
	Status             LiveOrderStatus
	StartTime          *time.Time
	EndTime            *time.Time
	RawData            string
	CreatedAt          time.Time
	UpdatedAt          time.Time
	PnLRatio           *float64
	PnLUSD             *float64
	CurrentPrice       *float64
	CurrentProfitRatio *float64
	CurrentProfitAbs   *float64
	UnrealizedPnLRatio *float64
	UnrealizedPnLUSD   *float64
	RealizedPnLRatio   *float64
	RealizedPnLUSD     *float64
	LastStatusSync     *time.Time
}

// OperationType 对应 trade_operation_log.operation。
type OperationType int

const (
	OperationOpen       OperationType = 1
	OperationTakeProfit OperationType = 5
	OperationStopLoss   OperationType = 6
	OperationAdjust     OperationType = 7
	OperationUpdatePlan OperationType = 8
	OperationFinalStop  OperationType = 9
	OperationFailed     OperationType = 10
	OperationForceExit  OperationType = 11
)

// TradeOperationRecord 表示一次仓位操作流水。
type TradeOperationRecord struct {
	FreqtradeID int
	Symbol      string
	Operation   OperationType
	Details     map[string]any
	Timestamp   time.Time
}

// EventRecord represents a persisted domain event.
type EventRecord struct {
	ID        string
	Type      string
	Payload   []byte
	CreatedAt time.Time
	TradeID   int
	Symbol    string
}
