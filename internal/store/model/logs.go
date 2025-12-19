package model

import "gorm.io/datatypes"

type TradeOperationModel struct {
	ID          int64          `gorm:"column:id;primaryKey"`
	FreqtradeID int            `gorm:"column:freqtrade_id"`
	Symbol      string         `gorm:"column:symbol"`
	Operation   int            `gorm:"column:operation"`
	Details     datatypes.JSON `gorm:"column:details"`
	Timestamp   int64          `gorm:"column:timestamp"`
}

func (TradeOperationModel) TableName() string { return "trade_operation_log" }
