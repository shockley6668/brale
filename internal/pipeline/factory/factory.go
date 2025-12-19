package factory

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"brale/internal/config/loader"
	"brale/internal/logger"
	"brale/internal/pipeline"
	"brale/internal/pipeline/middlewares"
	"brale/internal/store"
)

type Factory struct {
	Exporter         store.SnapshotExporter
	DefaultIntervals []string
	DefaultLimit     int
}

func (f *Factory) Build(cfg loader.MiddlewareConfig, profile loader.ProfileDefinition) (pipeline.Middleware, error) {
	name := strings.TrimSpace(cfg.Name)
	switch name {
	case "", "kline_fetcher":
		return f.buildCandleFetcher(cfg, profile)
	case "ema_trend":
		return f.buildEMATrend(cfg, profile)
	case "rsi_extreme":
		return f.buildRSI(cfg, profile)
	case "macd_trend":
		return f.buildMACD(cfg, profile)
	default:
		return nil, fmt.Errorf("unknown middleware: %s", cfg.Name)
	}
}

func (f *Factory) buildCandleFetcher(cfg loader.MiddlewareConfig, profile loader.ProfileDefinition) (pipeline.Middleware, error) {
	intervals := sliceFromCfg(cfg.Params, "intervals")
	if len(intervals) == 0 {
		intervals = profile.IntervalsLower()
	}
	if len(intervals) == 0 {
		return nil, fmt.Errorf("kline_fetcher 缺少 intervals")
	}
	limit := intFromCfg(cfg.Params, "limit")
	if limit <= 0 {
		if f.DefaultLimit > 0 {
			limit = f.DefaultLimit
		}
	}
	if limit <= 0 {
		return nil, fmt.Errorf("kline_fetcher 缺少有效的 limit")
	}
	mw := middlewares.NewCandleFetcher(middlewares.CandleFetcherConfig{
		Name:      cfg.Name,
		Stage:     cfg.Stage,
		Critical:  cfg.Critical,
		Timeout:   time.Duration(cfg.TimeoutSeconds) * time.Second,
		Intervals: intervals,
		Limit:     limit,
	}, f.Exporter)
	return mw, nil
}

func (f *Factory) buildEMATrend(cfg loader.MiddlewareConfig, profile loader.ProfileDefinition) (pipeline.Middleware, error) {
	interval := stringFromCfg(cfg.Params, "interval")
	if interval == "" {
		if ints := profile.IntervalsLower(); len(ints) > 0 {
			interval = ints[0]
		}
	}
	if interval == "" {
		return nil, fmt.Errorf("ema_trend 缺少 interval")
	}
	fast := intFromCfg(cfg.Params, "fast")
	mid := intFromCfg(cfg.Params, "mid")
	slow := intFromCfg(cfg.Params, "slow")
	if fast <= 0 || mid <= 0 || slow <= 0 {
		return nil, fmt.Errorf("ema_trend 需设置 fast/mid/slow")
	}
	mw := middlewares.NewEMATrend(middlewares.EMATrendConfig{
		Name:     cfg.Name,
		Stage:    cfg.Stage,
		Critical: cfg.Critical,
		Timeout:  time.Duration(cfg.TimeoutSeconds) * time.Second,
		Interval: interval,
		Fast:     fast,
		Mid:      mid,
		Slow:     slow,
	})
	return mw, nil
}

func (f *Factory) buildRSI(cfg loader.MiddlewareConfig, profile loader.ProfileDefinition) (pipeline.Middleware, error) {
	interval := stringFromCfg(cfg.Params, "interval")
	if interval == "" {
		ints := profile.IntervalsLower()
		if len(ints) > 0 {
			interval = ints[0]
		}
	}
	if interval == "" {
		return nil, fmt.Errorf("rsi_extreme 缺少 interval")
	}
	period := intFromCfg(cfg.Params, "period")
	if period <= 0 {
		return nil, fmt.Errorf("rsi_extreme 缺少 period")
	}
	overbought := floatFromCfg(cfg.Params, "overbought")
	if overbought == 0 {
		return nil, fmt.Errorf("rsi_extreme 缺少 overbought")
	}
	oversold := floatFromCfg(cfg.Params, "oversold")
	if oversold == 0 {
		return nil, fmt.Errorf("rsi_extreme 缺少 oversold")
	}
	mw := middlewares.NewRSIMiddleware(middlewares.RSIConfig{
		Name:       cfg.Name,
		Stage:      cfg.Stage,
		Critical:   cfg.Critical,
		Timeout:    time.Duration(cfg.TimeoutSeconds) * time.Second,
		Interval:   interval,
		Period:     period,
		Overbought: overbought,
		Oversold:   oversold,
	})
	return mw, nil
}

func (f *Factory) buildMACD(cfg loader.MiddlewareConfig, profile loader.ProfileDefinition) (pipeline.Middleware, error) {
	interval := stringFromCfg(cfg.Params, "interval")
	if interval == "" {
		ints := profile.IntervalsLower()
		if len(ints) > 0 {
			interval = ints[0]
		}
	}
	if interval == "" {
		return nil, fmt.Errorf("macd_trend 缺少 interval")
	}
	fast := intFromCfg(cfg.Params, "fast")
	slow := intFromCfg(cfg.Params, "slow")
	signal := intFromCfg(cfg.Params, "signal")
	if fast <= 0 {
		fast = 12
	}
	if slow <= 0 {
		slow = 26
	}
	if signal <= 0 {
		signal = 9
	}
	if fast >= slow {
		return nil, fmt.Errorf("macd_trend fast 需小于 slow")
	}
	mw := middlewares.NewMACDMiddleware(middlewares.MACDConfig{
		Name:     cfg.Name,
		Stage:    cfg.Stage,
		Critical: cfg.Critical,
		Timeout:  time.Duration(cfg.TimeoutSeconds) * time.Second,
		Interval: interval,
		Fast:     fast,
		Slow:     slow,
		Signal:   signal,
	})
	return mw, nil
}

func sliceFromCfg(params map[string]interface{}, key string) []string {
	if params == nil {
		return nil
	}
	raw, ok := params[key]
	if !ok {
		return nil
	}
	switch val := raw.(type) {
	case []string:
		return val
	case []interface{}:
		out := make([]string, 0, len(val))
		for _, item := range val {
			str := strings.TrimSpace(fmt.Sprintf("%v", item))
			if str == "" {
				continue
			}
			out = append(out, str)
		}
		return out
	default:
		parts := strings.Split(fmt.Sprintf("%v", val), ",")
		out := make([]string, 0, len(parts))
		for _, item := range parts {
			s := strings.TrimSpace(item)
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	}
}

func stringFromCfg(params map[string]interface{}, key string) string {
	if params == nil {
		return ""
	}
	raw, ok := params[key]
	if !ok {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", raw))
}

func intFromCfg(params map[string]interface{}, key string) int {
	if params == nil {
		return 0
	}
	raw, ok := params[key]
	if !ok {
		return 0
	}
	switch v := raw.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		val, err := strconv.Atoi(fmt.Sprintf("%v", v))
		if err != nil {
			logger.Warnf("middleware param %s invalid int: %v", key, err)
			return 0
		}
		return val
	}
}

func floatFromCfg(params map[string]interface{}, key string) float64 {
	if params == nil {
		return 0
	}
	raw, ok := params[key]
	if !ok {
		return 0
	}
	switch v := raw.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	default:
		val, err := strconv.ParseFloat(fmt.Sprintf("%v", v), 64)
		if err != nil {
			logger.Warnf("middleware param %s invalid float: %v", key, err)
			return 0
		}
		return val
	}
}
