package sqlite

import (
	"context"
	"errors"

	"brale/internal/store/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type strategyRepo struct {
	db *gorm.DB
}

func NewStrategyRepo(db *gorm.DB) *strategyRepo {
	return &strategyRepo{db: db}
}

func (r *strategyRepo) Save(ctx context.Context, strategy *model.StrategyInstanceModel) error {
	if strategy == nil {
		return errors.New("strategy cannot be nil")
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "trade_id"}, {Name: "plan_id"}, {Name: "plan_component"}},
		UpdateAll: true,
	}).Save(strategy).Error
}

func (r *strategyRepo) FindActiveByTradeID(ctx context.Context, tradeID int) ([]model.StrategyInstanceModel, error) {
	var strategies []model.StrategyInstanceModel
	err := r.db.WithContext(ctx).
		Where("trade_id = ? AND status != ?", tradeID, model.StrategyStatusDone).
		Find(&strategies).Error
	return strategies, err
}

func (r *strategyRepo) FindByTradeID(ctx context.Context, tradeID int) ([]model.StrategyInstanceModel, error) {
	var strategies []model.StrategyInstanceModel
	err := r.db.WithContext(ctx).
		Where("trade_id = ?", tradeID).
		Find(&strategies).Error
	return strategies, err
}

func (r *strategyRepo) UpdateStatus(ctx context.Context, tradeID int, planID, planComponent string, status model.StrategyStatus) error {
	return r.db.WithContext(ctx).Model(&model.StrategyInstanceModel{}).
		Where("trade_id = ? AND plan_id = ? AND plan_component = ?", tradeID, planID, planComponent).
		Update("status", status).Error
}
