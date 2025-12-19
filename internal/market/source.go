package market

import "context"

type CandleEvent struct {
	Symbol   string
	Interval string
	Candle   Candle
}

type TickEvent struct {
	Symbol    string
	Price     float64
	Quantity  float64
	EventTime int64
	TradeTime int64
}

type OpenInterestPoint struct {
	Symbol               string  `json:"symbol"`
	SumOpenInterest      float64 `json:"sumOpenInterest"`
	SumOpenInterestValue float64 `json:"sumOpenInterestValue"`
	Timestamp            int64   `json:"timestamp"`
}

type SubscribeOptions struct {
	BatchSize    int
	Buffer       int
	OnConnect    func()
	OnDisconnect func(error)
}

type SourceStats struct {
	Reconnects      int
	SubscribeErrors int
	LastError       string
}

type Source interface {
	FetchHistory(ctx context.Context, symbol, interval string, limit int) ([]Candle, error)

	Subscribe(ctx context.Context, symbols, intervals []string, opts SubscribeOptions) (<-chan CandleEvent, error)

	SubscribeTrades(ctx context.Context, symbols []string, opts SubscribeOptions) (<-chan TickEvent, error)

	GetFundingRate(ctx context.Context, symbol string) (float64, error)

	GetOpenInterestHistory(ctx context.Context, symbol, period string, limit int) ([]OpenInterestPoint, error)

	Stats() SourceStats

	Close() error
}
