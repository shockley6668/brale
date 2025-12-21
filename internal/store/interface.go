package store

import (
	"context"

	"brale/internal/store/model"
)

// UnitOfWork wraps a DB transaction. Commit or Rollback must be called.
type UnitOfWork interface {
	Commit() error
	Rollback() error
	Orders() OrderRepository
	Strategies() StrategyRepository
	Logs() LogRepository
}

// ReadUnitOfWork provides read-only repositories without starting a transaction.
type ReadUnitOfWork interface {
	Close() error
	Orders() ReadOrderRepository
	Strategies() ReadStrategyRepository
	Logs() ReadLogRepository
}

// Store is the entry point for database operations.
type Store interface {
	Begin(ctx context.Context) (UnitOfWork, error)
	Read(ctx context.Context) (ReadUnitOfWork, error)
	Close() error
}

// OrderRepository persists order records from freqtrade webhooks.
type OrderRepository interface {
	ReadOrderRepository
	Save(ctx context.Context, order *model.LiveOrderModel) error
}

// ReadOrderRepository provides read-only access to orders.
type ReadOrderRepository interface {
	FindByID(ctx context.Context, id int) (*model.LiveOrderModel, error)
	ListActive(ctx context.Context) ([]model.LiveOrderModel, error)
	ListRecent(ctx context.Context, limit int) ([]model.LiveOrderModel, error)
}

// StrategyRepository manages exit strategy instance records.
// Status transitions: Active -> Pending -> Done (or Cancelled).
type StrategyRepository interface {
	ReadStrategyRepository
	Save(ctx context.Context, strategy *model.StrategyInstanceModel) error
	UpdateStatus(ctx context.Context, tradeID int, planID, planComponent string, status model.StrategyStatus) error
}

// ReadStrategyRepository provides read-only access to strategy instances.
type ReadStrategyRepository interface {
	FindActiveByTradeID(ctx context.Context, tradeID int) ([]model.StrategyInstanceModel, error)
	FindByTradeID(ctx context.Context, tradeID int) ([]model.StrategyInstanceModel, error)
}

// LogRepository stores trade operation history for audit/debugging.
type LogRepository interface {
	ReadLogRepository
	InsertTradeOperation(ctx context.Context, log *model.TradeOperationModel) error
}

// ReadLogRepository provides read-only access to trade operations.
type ReadLogRepository interface {
	ListTradeOperations(ctx context.Context, tradeID int, limit int) ([]model.TradeOperationModel, error)
}
