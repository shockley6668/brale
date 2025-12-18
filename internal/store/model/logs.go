package model

import "gorm.io/datatypes"

// TradeOperationModel maps to 'trade_operation_log' table.
type TradeOperationModel struct {
	ID          int64          `gorm:"column:id;primaryKey"`
	FreqtradeID int            `gorm:"column:freqtrade_id"`
	Symbol      string         `gorm:"column:symbol"`
	Operation   int            `gorm:"column:operation"` // Maps to database.OperationType
	Details     datatypes.JSON `gorm:"column:details"`
	Timestamp   int64          `gorm:"column:timestamp"`
}

func (TradeOperationModel) TableName() string { return "trade_operation_log" }
