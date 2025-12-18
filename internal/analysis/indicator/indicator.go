package indicator

import (
	"fmt"
	"math"

	"github.com/markcheno/go-talib"

	"brale/internal/market"
)

// Settings 描述计算指标所需的最小配置。
type Settings struct {
	Symbol   string
	Interval string
	EMA      EMASettings
	RSI      RSISettings
}

// EMASettings 描述 EMA 指标参数。
type EMASettings struct {
	Fast int `json:"fast,omitempty"`
	Mid  int `json:"mid,omitempty"`
	Slow int `json:"slow,omitempty"`
}

// RSISettings 描述 RSI 指标参数。
type RSISettings struct {
	Period     int     `json:"period,omitempty"`
	Oversold   float64 `json:"oversold,omitempty"`
	Overbought float64 `json:"overbought,omitempty"`
}

// IndicatorValue 保存单个指标的最新值、序列与状态。
type IndicatorValue struct {
	Latest float64   `json:"latest"`
	Series []float64 `json:"series,omitempty"`
	State  string    `json:"state,omitempty"`
	Note   string    `json:"note,omitempty"`
}

// Report 汇总单个 symbol+interval 的指标输出。
type Report struct {
	Symbol   string                    `json:"symbol"`
	Interval string                    `json:"interval"`
	Count    int                       `json:"count"`
	Values   map[string]IndicatorValue `json:"values"`
	Warnings []string                  `json:"warnings,omitempty"`
}

// ComputeAll 计算常用指标并返回结构化报告。
func ComputeAll(candles []market.Candle, cfg Settings) (Report, error) {
	rep := Report{
		Symbol:   cfg.Symbol,
		Interval: cfg.Interval,
		Count:    len(candles),
		Values:   make(map[string]IndicatorValue),
	}
	if len(candles) == 0 {
		return rep, fmt.Errorf("no candles")
	}
	closes := make([]float64, len(candles))
	highs := make([]float64, len(candles))
	lows := make([]float64, len(candles))
	volumes := make([]float64, len(candles))
	for i, c := range candles {
		closes[i] = c.Close
		highs[i] = c.High
		lows[i] = c.Low
		volumes[i] = c.Volume
	}

	// EMA
	if cfg.EMA.Fast <= 0 {
		cfg.EMA.Fast = 21
	}
	if cfg.EMA.Mid <= 0 {
		cfg.EMA.Mid = 50
	}
	if cfg.EMA.Slow <= 0 {
		cfg.EMA.Slow = 200
	}
	emaFast := trimEMALeadingZeros(sanitizeSeries(talib.Ema(closes, cfg.EMA.Fast)))
	emaMid := trimEMALeadingZeros(sanitizeSeries(talib.Ema(closes, cfg.EMA.Mid)))
	emaSlow := trimEMALeadingZeros(sanitizeSeries(talib.Ema(closes, cfg.EMA.Slow)))
	lastClose := closes[len(closes)-1]
	rep.Values["ema_fast"] = IndicatorValue{
		Latest: lastValid(emaFast),
		Series: emaFast,
		State:  relativeState(lastClose, lastValid(emaFast)),
		Note:   fmt.Sprintf("EMA%d vs price", cfg.EMA.Fast),
	}
	rep.Values["ema_mid"] = IndicatorValue{
		Latest: lastValid(emaMid),
		Series: emaMid,
		State:  relativeState(lastClose, lastValid(emaMid)),
		Note:   fmt.Sprintf("EMA%d vs price", cfg.EMA.Mid),
	}
	rep.Values["ema_slow"] = IndicatorValue{
		Latest: lastValid(emaSlow),
		Series: emaSlow,
		State:  relativeState(lastClose, lastValid(emaSlow)),
		Note:   fmt.Sprintf("EMA%d vs price", cfg.EMA.Slow),
	}

	// RSI
	if cfg.RSI.Period <= 0 {
		cfg.RSI.Period = 14
	}
	if cfg.RSI.Overbought == 0 {
		cfg.RSI.Overbought = 70
	}
	if cfg.RSI.Oversold == 0 {
		cfg.RSI.Oversold = 30
	}
	rsiSeries := sanitizeSeries(talib.Rsi(closes, cfg.RSI.Period))
	rsiVal := lastValid(rsiSeries)
	state := "neutral"
	switch {
	case rsiVal >= cfg.RSI.Overbought:
		state = "overbought"
	case rsiVal <= cfg.RSI.Oversold:
		state = "oversold"
	}
	rep.Values["rsi"] = IndicatorValue{
		Latest: rsiVal,
		Series: rsiSeries,
		State:  state,
		Note:   fmt.Sprintf("period=%d thresholds=%.1f/%.1f", cfg.RSI.Period, cfg.RSI.Oversold, cfg.RSI.Overbought),
	}

	// MACD
	macd, signal, hist := talib.Macd(closes, 12, 26, 9)
	macdSeries := sanitizeSeries(macd)
	signalSeries := sanitizeSeries(signal)
	histSeries := sanitizeSeries(hist)
	macdNote := fmt.Sprintf("signal=%.4f hist=%.4f", lastValid(signalSeries), lastValid(histSeries))
	macdState := "flat"
	switch {
	case lastValid(histSeries) > 0:
		macdState = "bullish"
	case lastValid(histSeries) < 0:
		macdState = "bearish"
	}
	rep.Values["macd"] = IndicatorValue{
		Latest: lastValid(macdSeries),
		Series: histSeries,
		State:  macdState,
		Note:   macdNote,
	}

	// ROC
	rocSeries := sanitizeSeries(talib.Roc(closes, 9))
	rocVal := lastValid(rocSeries)
	rep.Values["roc"] = IndicatorValue{
		Latest: rocVal,
		Series: rocSeries,
		State:  polarityState(rocVal),
		Note:   "period=9",
	}

	// Stochastic Oscillator
	k, d := talib.Stoch(highs, lows, closes, 14, 3, talib.SMA, 3, talib.SMA)
	kSeries := sanitizeSeries(k)
	dSeries := sanitizeSeries(d)
	rep.Values["stoch_k"] = IndicatorValue{
		Latest: lastValid(kSeries),
		Series: kSeries,
		State:  stochasticState(lastValid(kSeries)),
		Note:   fmt.Sprintf("d=%.2f", lastValid(dSeries)),
	}

	// Williams %R
	will := sanitizeSeries(talib.WillR(highs, lows, closes, 14))
	rep.Values["williams_r"] = IndicatorValue{
		Latest: lastValid(will),
		Series: will,
		State:  stochasticState(-lastValid(will)),
		Note:   "period=14",
	}

	// ATR
	atrSeries := sanitizeSeries(talib.Atr(highs, lows, closes, 14))
	rep.Values["atr"] = IndicatorValue{
		Latest: lastValid(atrSeries),
		Series: atrSeries,
		State:  "volatility",
		Note:   "period=14",
	}

	// OBV style volume thrust
	obv := sanitizeSeries(talib.Obv(closes, volumes))
	rep.Values["obv"] = IndicatorValue{
		Latest: lastValid(obv),
		Series: obv,
		State:  polarityState(lastValid(rocSeries)),
		Note:   "volume thrust",
	}

	return rep, nil
}

// ComputeATRSeries 单独计算 ATR 序列，便于在禁用其它指标时仍可获取波动率数据。
func ComputeATRSeries(candles []market.Candle, period int) ([]float64, error) {
	if len(candles) == 0 {
		return nil, fmt.Errorf("no candles")
	}
	if period <= 0 {
		period = 14
	}
	highs := make([]float64, len(candles))
	lows := make([]float64, len(candles))
	closes := make([]float64, len(candles))
	for i, c := range candles {
		highs[i] = c.High
		lows[i] = c.Low
		closes[i] = c.Close
	}
	series := sanitizeSeries(talib.Atr(highs, lows, closes, period))
	if len(series) == 0 {
		return nil, fmt.Errorf("atr series empty")
	}
	return series, nil
}

func sanitizeSeries(src []float64) []float64 {
	out := make([]float64, 0, len(src))
	for _, v := range src {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		out = append(out, round4(v))
	}
	return out
}

// trimEMALeadingZeros drops TALib's zero-seeded EMA values so plots start when enough candles exist.
func trimEMALeadingZeros(series []float64) []float64 {
	start := 0
	for start < len(series) && almostZero(series[start]) {
		start++
	}
	return series[start:]
}

func almostZero(v float64) bool {
	return math.Abs(v) <= 1e-9
}

func lastValid(series []float64) float64 {
	for i := len(series) - 1; i >= 0; i-- {
		if !math.IsNaN(series[i]) && !math.IsInf(series[i], 0) {
			return series[i]
		}
	}
	return 0
}

func relativeState(price, ref float64) string {
	if ref == 0 {
		return "unknown"
	}
	switch {
	case price > ref*1.002:
		return "above"
	case price < ref*0.998:
		return "below"
	default:
		return "touch"
	}
}

func polarityState(v float64) string {
	switch {
	case v > 0:
		return "positive"
	case v < 0:
		return "negative"
	default:
		return "flat"
	}
}

func stochasticState(v float64) string {
	switch {
	case v >= 80:
		return "overbought"
	case v <= 20:
		return "oversold"
	default:
		return "neutral"
	}
}

func round4(v float64) float64 {
	return math.Round(v*10000) / 10000
}
