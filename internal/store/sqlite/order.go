package sqlite

import (
	"context"
	"errors"

	"brale/internal/store/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// orderRepository implements the OrderRepository interface.
type orderRepository struct {
	db *gorm.DB
}

// NewOrderRepo creates a new orderRepo.
func NewOrderRepo(db *gorm.DB) *orderRepository {
	return &orderRepository{db: db}
}

// Save saves or updates an order.
func (r *orderRepository) Save(ctx context.Context, order *model.LiveOrderModel) error {
	if order == nil {
		return errors.New("order cannot be nil")
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "freqtrade_id"}},
		UpdateAll: true,
	}).Save(order).Error
}

// FindByID finds an order by FreqtradeID.
func (r *orderRepository) FindByID(ctx context.Context, id int) (*model.LiveOrderModel, error) {
	var order model.LiveOrderModel
	err := r.db.WithContext(ctx).Where("freqtrade_id = ?", id).First(&order).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &order, nil
}

// ListActive lists all active orders (statuses: Open, Partial, Retrying, Opening, ClosingPartial, ClosingFull).
func (r *orderRepository) ListActive(ctx context.Context) ([]model.LiveOrderModel, error) {
	var orders []model.LiveOrderModel
	statuses := []int{
		int(model.LiveOrderStatusOpen),
		int(model.LiveOrderStatusPartial),
		int(model.LiveOrderStatusRetrying),
		int(model.LiveOrderStatusOpening),
		int(model.LiveOrderStatusClosingPartial),
		int(model.LiveOrderStatusClosingFull),
	}
	if err := r.db.WithContext(ctx).
		Where("status IN ?", statuses).
		Order("start_timestamp DESC, id DESC").
		Find(&orders).Error; err != nil {
		return nil, err
	}
	return orders, nil
}

// ListRecent lists recent orders.
func (r *orderRepository) ListRecent(ctx context.Context, limit int) ([]model.LiveOrderModel, error) {
	var orders []model.LiveOrderModel
	if limit <= 0 {
		limit = 100
	}
	if err := r.db.WithContext(ctx).
		Order("COALESCE(end_timestamp, start_timestamp, created_at) DESC, id DESC").
		Limit(limit).
		Find(&orders).Error; err != nil {
		return nil, err
	}
	return orders, nil
}