package store

import (
	"context"

	"brale/internal/store/model"
)

// UnitOfWork defines a transaction scope.
type UnitOfWork interface {
	// Commit commits the transaction.
	Commit() error
	// Rollback rolls back the transaction.
	Rollback() error

	// Orders returns the order repository within this transaction.
	Orders() OrderRepository
	// Strategies returns the strategy repository within this transaction.
	Strategies() StrategyRepository
	// Logs returns the log repository within this transaction.
	Logs() LogRepository
}

// Store is the entry point for database access.
type Store interface {
	// Begin starts a new UnitOfWork (transaction).
	Begin(ctx context.Context) (UnitOfWork, error)
	// Close closes the store connection.
	Close() error
}

// OrderRepository handles order persistence.
type OrderRepository interface {
	Save(ctx context.Context, order *model.LiveOrderModel) error
	FindByID(ctx context.Context, id int) (*model.LiveOrderModel, error)
	ListActive(ctx context.Context) ([]model.LiveOrderModel, error)
	ListRecent(ctx context.Context, limit int) ([]model.LiveOrderModel, error)
}

// StrategyRepository handles strategy instance persistence.
type StrategyRepository interface {
	Save(ctx context.Context, strategy *model.StrategyInstanceModel) error
	FindActiveByTradeID(ctx context.Context, tradeID int) ([]model.StrategyInstanceModel, error)
	FindByTradeID(ctx context.Context, tradeID int) ([]model.StrategyInstanceModel, error)
	UpdateStatus(ctx context.Context, tradeID int, planID, planComponent string, status model.StrategyStatus) error
}

// LogRepository handles trade logs and events.
type LogRepository interface {
	ListTradeOperations(ctx context.Context, tradeID int, limit int) ([]model.TradeOperationModel, error)
	InsertTradeOperation(ctx context.Context, log *model.TradeOperationModel) error
}
