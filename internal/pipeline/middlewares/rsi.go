package middlewares

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"brale/internal/market"
	"brale/internal/pipeline"

	talib "github.com/markcheno/go-talib"
)

// RSIConfig 控制 RSI 参数。
type RSIConfig struct {
	Name       string
	Stage      int
	Critical   bool
	Timeout    time.Duration
	Interval   string
	Period     int
	Overbought float64
	Oversold   float64
}

// RSIMiddleware 输出 RSI 状态。
type RSIMiddleware struct {
	meta       pipeline.MiddlewareMeta
	interval   string
	period     int
	overbought float64
	oversold   float64
}

// NewRSIMiddleware 构造 RSI 中间件。
func NewRSIMiddleware(cfg RSIConfig) *RSIMiddleware {
	if cfg.Period <= 0 {
		cfg.Period = 14
	}
	if cfg.Overbought <= 0 {
		cfg.Overbought = 70
	}
	if cfg.Oversold <= 0 {
		cfg.Oversold = 30
	}
	return &RSIMiddleware{
		meta: pipeline.MiddlewareMeta{
			Name:     nameOrDefault(cfg.Name, "rsi_extreme"),
			Stage:    cfg.Stage,
			Critical: cfg.Critical,
			Timeout:  cfg.Timeout,
		},
		interval:   strings.ToLower(strings.TrimSpace(cfg.Interval)),
		period:     cfg.Period,
		overbought: cfg.Overbought,
		oversold:   cfg.Oversold,
	}
}

// Meta 实现接口。
func (m *RSIMiddleware) Meta() pipeline.MiddlewareMeta { return m.meta }

// Handle 计算 RSI。
func (m *RSIMiddleware) Handle(ctx context.Context, ac *pipeline.AnalysisContext) error {
	interval := m.interval
	if interval == "" {
		interval = "1h"
	}
	candles := ac.Candles(interval)
	if len(candles) < m.period+1 {
		return fmt.Errorf("rsi: insufficient candles %s need %d got %d", interval, m.period, len(candles))
	}
	closes := closes(candles)
	series := talib.Rsi(closes, m.period)
	if len(series) == 0 {
		return fmt.Errorf("rsi: talib output empty for %s", interval)
	}
	val := series[len(series)-1]
	status := "震荡"
	if val >= m.overbought {
		status = "超买"
	} else if val <= m.oversold {
		status = "超卖"
	}
	minVal, minIdx := math.MaxFloat64, -1
	maxVal, maxIdx := -math.MaxFloat64, -1
	for idx, v := range series {
		if v == 0 {
			continue
		}
		if v < minVal {
			minVal = v
			minIdx = idx
		}
		if v > maxVal {
			maxVal = v
			maxIdx = idx
		}
	}
	formatTime := func(idx int) string {
		if idx < 0 || idx >= len(candles) {
			return "n/a"
		}
		ts := candles[idx].CloseTime
		if ts == 0 {
			ts = candles[idx].OpenTime
		}
		if ts == 0 {
			return "n/a"
		}
		return time.UnixMilli(ts).UTC().Format(time.RFC3339)
	}
	desc := fmt.Sprintf("周期 %s 的 RSI(%d) 序列（长度 %d）已计算，当前值 %.2f",
		strings.ToUpper(interval), m.period, len(series), val)
	ac.AddFeature(pipeline.Feature{
		Key:         "rsi",
		Label:       fmt.Sprintf("%s RSI", strings.ToUpper(interval)),
		Value:       val,
		Description: formatFeature(ac.Symbol, desc),
		Metadata: map[string]any{
			"interval":      interval,
			"period":        m.period,
			"overbought":    m.overbought,
			"oversold":      m.oversold,
			"status":        status,
			"max_value":     maxVal,
			"max_timestamp": formatTime(maxIdx),
			"min_value":     minVal,
			"min_timestamp": formatTime(minIdx),
			"latest_value":  val,
			"latest_time":   formatTime(len(series) - 1),
			"series_tail":   seriesTail(series, 5),
			"status_label":  status,
			"pivots":        rsiPivots(series, candles),
		},
	})
	return nil
}

type rsiPivot struct {
	Type string  `json:"type"`
	Time string  `json:"time"`
	Val  float64 `json:"value"`
}

func seriesTail(series []float64, length int) []float64 {
	if length <= 0 || len(series) == 0 {
		return nil
	}
	if len(series) <= length {
		return append([]float64(nil), series...)
	}
	return append([]float64(nil), series[len(series)-length:]...)
}

func rsiPivots(series []float64, candles []market.Candle) []rsiPivot {
	if len(series) == 0 {
		return nil
	}
	pivots := make([]rsiPivot, 0, 6)
	add := func(label string, idx int, val float64) {
		if idx < 0 || idx >= len(candles) {
			return
		}
		ts := candles[idx].CloseTime
		if ts == 0 {
			ts = candles[idx].OpenTime
		}
		pivots = append(pivots, rsiPivot{
			Type: label,
			Time: time.UnixMilli(ts).UTC().Format(time.RFC3339),
			Val:  val,
		})
	}
	for i := len(series) - 2; i > 0 && len(pivots) < 6; i-- {
		cur := series[i]
		prev := series[i-1]
		next := series[i+1]
		if cur == 0 || prev == 0 || next == 0 {
			continue
		}
		if cur >= prev && cur >= next {
			add("peak", len(candles)-(len(series)-i), cur)
		} else if cur <= prev && cur <= next {
			add("trough", len(candles)-(len(series)-i), cur)
		}
	}
	return pivots
}
