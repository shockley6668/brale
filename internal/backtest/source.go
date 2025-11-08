package backtest

import (
	"brale/internal/market"
	"context"
)

// FetchRequest 描述一次远端 K 线请求。
type FetchRequest struct {
	Symbol   string
	Interval string
	Start    int64 // Unix ms
	End      int64 // Unix ms（可选；0 表示不限制）
	Limit    int
}

// CandleSource 统一不同交易所/数据源的拉取行为。
type CandleSource interface {
	Fetch(ctx context.Context, req FetchRequest) ([]market.Candle, error)
	Name() string
}
