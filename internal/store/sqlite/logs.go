package sqlite

import (
	"context"

	"brale/internal/store/model"

	"gorm.io/gorm"
)

type logRepo struct {
	db *gorm.DB
}

func NewLogRepo(db *gorm.DB) *logRepo {
	return &logRepo{db: db}
}

func (r *logRepo) ListTradeOperations(ctx context.Context, tradeID int, limit int) ([]model.TradeOperationModel, error) {
	var logs []model.TradeOperationModel
	q := r.db.WithContext(ctx).Where("freqtrade_id = ?", tradeID).Order("timestamp DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Find(&logs).Error; err != nil {
		return nil, err
	}
	return logs, nil
}

func (r *logRepo) InsertTradeOperation(ctx context.Context, log *model.TradeOperationModel) error {
	return r.db.WithContext(ctx).Create(log).Error
}
