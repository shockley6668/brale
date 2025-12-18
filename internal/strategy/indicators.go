package strategy

import talib "github.com/markcheno/go-talib"

// EMA 计算最近值（period>0 且 len(prices)>=period）。
func EMA(prices []float64, period int) float64 {
	if period <= 0 || len(prices) < period {
		return 0
	}
	out := talib.Ema(prices, period)
	if len(out) == 0 {
		return 0
	}
	return out[len(out)-1]
}

// MACD = EMA12 - EMA26（取 talib.Macd 的 macd line）
func MACD(prices []float64) float64 {
	if len(prices) < 26 {
		return 0
	}
	macd, _, _ := talib.Macd(prices, 12, 26, 9)
	if len(macd) == 0 {
		return 0
	}
	return macd[len(macd)-1]
}

// RSI（Wilder）
func RSI(prices []float64, period int) float64 {
	if period <= 0 || len(prices) <= period {
		return 0
	}
	out := talib.Rsi(prices, period)
	if len(out) == 0 {
		return 0
	}
	return out[len(out)-1]
}

// ClassifyTrend 根据 EMA 的排列判断趋势。
func ClassifyTrend(fast, mid, slow float64) string {
	switch {
	case fast >= mid && mid >= slow:
		return "UP"
	case fast <= mid && mid <= slow:
		return "DOWN"
	default:
		return "MIXED"
	}
}