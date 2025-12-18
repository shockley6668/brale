package middlewares

import (
	"context"
	"fmt"
	"strings"
	"time"

	"brale/internal/market"
	"brale/internal/pipeline"

	talib "github.com/markcheno/go-talib"
)

// MACDConfig 定义 MACD 中间件参数。
type MACDConfig struct {
	Name     string
	Stage    int
	Critical bool
	Timeout  time.Duration
	Interval string
	Fast     int
	Slow     int
	Signal   int
}

// MACDMiddleware 将 MACD 输出为特征。
type MACDMiddleware struct {
	meta     pipeline.MiddlewareMeta
	interval string
	fast     int
	slow     int
	signal   int
}

// NewMACDMiddleware 构造实例。
func NewMACDMiddleware(cfg MACDConfig) *MACDMiddleware {
	if cfg.Fast <= 0 {
		cfg.Fast = 12
	}
	if cfg.Slow <= 0 {
		cfg.Slow = 26
	}
	if cfg.Signal <= 0 {
		cfg.Signal = 9
	}
	return &MACDMiddleware{
		meta: pipeline.MiddlewareMeta{
			Name:     nameOrDefault(cfg.Name, "macd_trend"),
			Stage:    cfg.Stage,
			Critical: cfg.Critical,
			Timeout:  cfg.Timeout,
		},
		interval: strings.ToLower(strings.TrimSpace(cfg.Interval)),
		fast:     cfg.Fast,
		slow:     cfg.Slow,
		signal:   cfg.Signal,
	}
}

// Meta 实现接口。
func (m *MACDMiddleware) Meta() pipeline.MiddlewareMeta { return m.meta }

// Handle 计算 MACD。
func (m *MACDMiddleware) Handle(ctx context.Context, ac *pipeline.AnalysisContext) error {
	interval := m.interval
	if interval == "" {
		interval = "1h"
	}
	candles := ac.Candles(interval)
	required := m.slow + m.signal
	if len(candles) < required {
		return fmt.Errorf("macd_trend: %s 蜡烛不足，需 >= %d", interval, required)
	}
	closes := closes(candles)
	macd, signal, hist := talib.Macd(closes, m.fast, m.slow, m.signal)
	if len(macd) == 0 {
		return fmt.Errorf("macd_trend: 无法计算 %s MACD", interval)
	}
	idx := len(macd) - 1
	macdVal := macd[idx]
	signalVal := signal[idx]
	histVal := hist[idx]
	desc := fmt.Sprintf("周期 %s 的 MACD(%d/%d/%d) 序列（长度 %d）已计算，当前 macd=%.4f，signal=%.4f，hist=%.4f",
		strings.ToUpper(interval), m.fast, m.slow, m.signal, len(macd), macdVal, signalVal, histVal)
	ac.AddFeature(pipeline.Feature{
		Key:         "macd_trend",
		Label:       fmt.Sprintf("%s MACD", strings.ToUpper(interval)),
		Value:       histVal,
		Description: formatFeature(ac.Symbol, desc),
		Metadata: map[string]any{
			"interval":    interval,
			"macd_line":   macd,
			"signal_line": signal,
			"hist_line":   hist,
			"crossovers":  macdCrossovers(hist, candles),
		},
	})
	return nil
}

type macdPivot struct {
	Type string  `json:"type"`
	Time string  `json:"time"`
	Val  float64 `json:"value"`
}

func macdCrossovers(hist []float64, candles []market.Candle) []macdPivot {
	if len(hist) == 0 {
		return nil
	}
	out := make([]macdPivot, 0, 8)
	for i := len(hist) - 1; i > 0 && len(out) < 8; i-- {
		cur := hist[i]
		prev := hist[i-1]
		if cur*prev <= 0 && (cur-prev) != 0 {
			label := "死叉"
			if cur > 0 {
				label = "金叉"
			}
			pos := len(candles) - (len(hist) - i)
			if pos >= 0 && pos < len(candles) {
				ts := candles[pos].CloseTime
				if ts == 0 {
					ts = candles[pos].OpenTime
				}
				if ts != 0 {
					out = append(out, macdPivot{
						Type: label,
						Time: time.UnixMilli(ts).UTC().Format(time.RFC3339),
						Val:  cur,
					})
				}
			}
		}
	}
	return out
}
