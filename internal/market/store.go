package market

import (
	"context"
)

// KlineStore defines the interface for a kline store.
type KlineStore interface {
	Get(ctx context.Context, symbol, interval string) ([]Candle, error)
	Set(ctx context.Context, symbol, interval string, klines []Candle) error
	Put(ctx context.Context, symbol, interval string, klines []Candle, max int) error
}