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

// Store is the entry point for database operations.
type Store interface {
	Begin(ctx context.Context) (UnitOfWork, error)
	Close() error
}

// OrderRepository persists order records from freqtrade webhooks.
type OrderRepository interface {
	Save(ctx context.Context, order *model.LiveOrderModel) error
	FindByID(ctx context.Context, id int) (*model.LiveOrderModel, error)
	ListActive(ctx context.Context) ([]model.LiveOrderModel, error)
	ListRecent(ctx context.Context, limit int) ([]model.LiveOrderModel, error)
}

// StrategyRepository manages exit strategy instance records.
// Status transitions: Active -> Pending -> Done (or Cancelled).
type StrategyRepository interface {
	Save(ctx context.Context, strategy *model.StrategyInstanceModel) error
	FindActiveByTradeID(ctx context.Context, tradeID int) ([]model.StrategyInstanceModel, error)
	FindByTradeID(ctx context.Context, tradeID int) ([]model.StrategyInstanceModel, error)
	UpdateStatus(ctx context.Context, tradeID int, planID, planComponent string, status model.StrategyStatus) error
}

// LogRepository stores trade operation history for audit/debugging.
type LogRepository interface {
	ListTradeOperations(ctx context.Context, tradeID int, limit int) ([]model.TradeOperationModel, error)
	InsertTradeOperation(ctx context.Context, log *model.TradeOperationModel) error
}
