package sqlite

import (
	"context"
	"errors" // This import is removed in the provided Code Edit, but the instruction is to translate comments. The Code Edit seems to be a full replacement.

	// This import is removed in the provided Code Edit.
	"brale/internal/store/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// strategyRepo implements the StrategyRepository interface.
type strategyRepo struct {
	db *gorm.DB
}

// NewStrategyRepo creates a new strategyRepo.
func NewStrategyRepo(db *gorm.DB) *strategyRepo {
	return &strategyRepo{db: db}
}

// Save saves or updates a strategy instance.
func (r *strategyRepo) Save(ctx context.Context, strategy *model.StrategyInstanceModel) error {
	if strategy == nil {
		return errors.New("strategy cannot be nil")
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "trade_id"}, {Name: "plan_id"}, {Name: "plan_component"}},
		UpdateAll: true,
	}).Save(strategy).Error
}

// FindActiveByTradeID finds all unfinished strategy instances for a given TradeID.
func (r *strategyRepo) FindActiveByTradeID(ctx context.Context, tradeID int) ([]model.StrategyInstanceModel, error) {
	var strategies []model.StrategyInstanceModel
	err := r.db.WithContext(ctx).
		Where("trade_id = ? AND status != ?", tradeID, model.StrategyStatusDone).
		Find(&strategies).Error
	return strategies, err
}

// FindByTradeID finds all strategy instances for a given TradeID.
func (r *strategyRepo) FindByTradeID(ctx context.Context, tradeID int) ([]model.StrategyInstanceModel, error) {
	var strategies []model.StrategyInstanceModel
	err := r.db.WithContext(ctx).
		Where("trade_id = ?", tradeID).
		Find(&strategies).Error
	return strategies, err
}

// UpdateStatus updates the status of a strategy instance.
func (r *strategyRepo) UpdateStatus(ctx context.Context, tradeID int, planID, planComponent string, status model.StrategyStatus) error {
	return r.db.WithContext(ctx).Model(&model.StrategyInstanceModel{}).
		Where("trade_id = ? AND plan_id = ? AND plan_component = ?", tradeID, planID, planComponent).
		Update("status", status).Error
}
