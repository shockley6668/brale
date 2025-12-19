package market

import (
	"context"
)

type KlineStore interface {
	Get(ctx context.Context, symbol, interval string) ([]Candle, error)
	Set(ctx context.Context, symbol, interval string, klines []Candle) error
	Put(ctx context.Context, symbol, interval string, klines []Candle, max int) error
}
