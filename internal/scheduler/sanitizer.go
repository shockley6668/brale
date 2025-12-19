package scheduler

import (
	"time"

	"brale/internal/market"
)

const DefaultBinanceKlineGrace = 10 * time.Second

func DropUnclosedBinanceKline(klines []market.Candle, interval time.Duration) []market.Candle {
	return dropUnclosedBinanceKlineAt(klines, interval, time.Now().UTC(), DefaultBinanceKlineGrace)
}

func dropUnclosedBinanceKlineAt(klines []market.Candle, interval time.Duration, now time.Time, grace time.Duration) []market.Candle {
	if len(klines) == 0 {
		return klines
	}
	if interval <= 0 {
		return klines
	}
	if grace < 0 {
		grace = 0
	}
	last := klines[len(klines)-1]
	if last.OpenTime <= 0 {
		return klines
	}
	closeTimeMs := last.OpenTime + interval.Milliseconds()
	cutoffMs := closeTimeMs + grace.Milliseconds()
	if now.UnixMilli() < cutoffMs {
		return klines[:len(klines)-1]
	}
	return klines
}
