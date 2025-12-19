package decision

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"brale/internal/analysis/indicator"
	"brale/internal/market"

	talib "github.com/markcheno/go-talib"
)

const indicatorSnapshotVersion = "indicator_snapshot_v1"

type indicatorSnapshot struct {
	Meta   snapshotMeta   `json:"_meta"`
	Market snapshotMarket `json:"market"`
	Data   snapshotData   `json:"data"`
}

type snapshotMeta struct {
	SeriesOrder string `json:"series_order"`
	SampledAt   string `json:"sampled_at"`
	Version     string `json:"version"`
}

type snapshotMarket struct {
	Symbol         string  `json:"symbol"`
	Interval       string  `json:"interval"`
	CurrentPrice   float64 `json:"current_price"`
	PriceTimestamp string  `json:"price_timestamp"`
}

type snapshotData struct {
	EMAFast *emaSnapshot   `json:"ema_fast,omitempty"`
	EMAMid  *emaSnapshot   `json:"ema_mid,omitempty"`
	EMASlow *emaSnapshot   `json:"ema_slow,omitempty"`
	MACD    *macdSnapshot  `json:"macd,omitempty"`
	RSI     *rsiSnapshot   `json:"rsi,omitempty"`
	OBV     *obvSnapshot   `json:"obv,omitempty"`
	StochK  *stochSnapshot `json:"stoch_k,omitempty"`
	ATR     *atrSnapshot   `json:"atr,omitempty"`
}

type emaSnapshot struct {
	Latest       float64   `json:"latest"`
	LastN        []float64 `json:"last_n,omitempty"`
	PeriodHigh   float64   `json:"period_high"`
	PeriodLow    float64   `json:"period_low"`
	DeltaToPrice float64   `json:"delta_to_price"`
	DeltaPct     float64   `json:"delta_pct"`
}

type macdSnapshot struct {
	DIF       float64         `json:"dif"`
	DEA       float64         `json:"dea"`
	Histogram *seriesSnapshot `json:"histogram,omitempty"`
}

type rsiSnapshot struct {
	Current        float64   `json:"current"`
	LastN          []float64 `json:"last_n,omitempty"`
	PeriodHigh     float64   `json:"period_high"`
	PeriodLow      float64   `json:"period_low"`
	DistanceToHigh float64   `json:"distance_to_high"`
	DistanceToLow  float64   `json:"distance_to_low"`
}

type obvSnapshot struct {
	Latest float64   `json:"latest"`
	LastN  []float64 `json:"last_n,omitempty"`
}

type stochSnapshot struct {
	Current float64   `json:"current"`
	LastN   []float64 `json:"last_n,omitempty"`
	RangeLo float64   `json:"range_min"`
	RangeHi float64   `json:"range_max"`
}

type seriesSnapshot struct {
	Last []float64 `json:"last_n,omitempty"`
}

type atrSnapshot struct {
	Latest  float64   `json:"latest"`
	LastN   []float64 `json:"last_n,omitempty"`
	RangeLo float64   `json:"range_min"`
	RangeHi float64   `json:"range_max"`
}

func BuildIndicatorSnapshot(candles []market.Candle, rep indicator.Report) ([]byte, error) {
	if len(candles) == 0 {
		return nil, fmt.Errorf("indicator snapshot: no candles")
	}
	if len(rep.Values) == 0 {
		return nil, fmt.Errorf("indicator snapshot: empty report")
	}
	last := candles[len(candles)-1]
	stamp := candleTimestamp(last)
	price := last.Close
	snapshot := indicatorSnapshot{
		Meta: snapshotMeta{
			SeriesOrder: "oldest_to_latest",
			SampledAt:   stamp,
			Version:     indicatorSnapshotVersion,
		},
		Market: snapshotMarket{
			Symbol:         strings.ToUpper(strings.TrimSpace(rep.Symbol)),
			Interval:       strings.ToLower(strings.TrimSpace(rep.Interval)),
			CurrentPrice:   roundFloat(price, 4),
			PriceTimestamp: stamp,
		},
	}
	data := snapshotData{}
	if val, ok := rep.Values["ema_fast"]; ok {
		data.EMAFast = buildEMASnapshot(val, price, 5)
	}
	if val, ok := rep.Values["ema_mid"]; ok {
		data.EMAMid = buildEMASnapshot(val, price, 4)
	}
	if val, ok := rep.Values["ema_slow"]; ok {
		data.EMASlow = buildEMASnapshot(val, price, 3)
	}
	if _, ok := rep.Values["macd"]; ok {
		if snap := buildMACDSnapshot(candles, 3); snap != nil {
			data.MACD = snap
		}
	}
	if val, ok := rep.Values["rsi"]; ok {
		data.RSI = buildRSISnapshot(val)
	}
	if val, ok := rep.Values["obv"]; ok {
		data.OBV = buildOBVSnapshot(val)
	}
	if val, ok := rep.Values["stoch_k"]; ok {
		data.StochK = buildStochSnapshot(val)
	}
	if val, ok := rep.Values["atr"]; ok {
		data.ATR = buildATRSnapshot(val)
	}
	snapshot.Data = data
	return json.Marshal(snapshot)
}

func buildEMASnapshot(val indicator.IndicatorValue, price float64, tail int) *emaSnapshot {
	if val.Latest == 0 && len(val.Series) == 0 {
		return nil
	}
	maxVal, minVal := seriesBounds(val.Series)
	delta := price - val.Latest
	deltaPct := 0.0
	if val.Latest != 0 {
		deltaPct = (delta / val.Latest) * 100
	}
	return &emaSnapshot{
		Latest:       roundFloat(val.Latest, 4),
		LastN:        roundSeriesTail(val.Series, tail),
		PeriodHigh:   roundFloat(maxVal, 4),
		PeriodLow:    roundFloat(minVal, 4),
		DeltaToPrice: roundFloat(delta, 4),
		DeltaPct:     roundFloat(deltaPct, 4),
	}
}

func buildMACDSnapshot(candles []market.Candle, tail int) *macdSnapshot {
	if len(candles) == 0 {
		return nil
	}
	closes := make([]float64, len(candles))
	for i, c := range candles {
		closes[i] = c.Close
	}
	macdSeries, signalSeries, histSeries := talib.Macd(closes, 12, 26, 9)
	mSeries := sanitizeSeries(macdSeries)
	sSeries := sanitizeSeries(signalSeries)
	hSeries := sanitizeSeries(histSeries)
	if len(mSeries) == 0 || len(sSeries) == 0 || len(hSeries) == 0 {
		return nil
	}
	histLast := roundSeriesTail(hSeries, tail)
	var hist *seriesSnapshot
	if len(histLast) > 0 {
		hist = &seriesSnapshot{Last: histLast}
	}
	return &macdSnapshot{
		DIF:       roundFloat(mSeries[len(mSeries)-1], 4),
		DEA:       roundFloat(sSeries[len(sSeries)-1], 4),
		Histogram: hist,
	}
}

func buildRSISnapshot(val indicator.IndicatorValue) *rsiSnapshot {
	if val.Latest == 0 && len(val.Series) == 0 {
		return nil
	}
	maxVal, minVal := seriesBounds(val.Series)
	return &rsiSnapshot{
		Current:        roundFloat(val.Latest, 4),
		LastN:          roundSeriesTail(val.Series, 3),
		PeriodHigh:     roundFloat(maxVal, 4),
		PeriodLow:      roundFloat(minVal, 4),
		DistanceToHigh: roundFloat(maxVal-val.Latest, 4),
		DistanceToLow:  roundFloat(val.Latest-minVal, 4),
	}
}

func buildOBVSnapshot(val indicator.IndicatorValue) *obvSnapshot {
	if len(val.Series) == 0 {
		return nil
	}
	return &obvSnapshot{
		Latest: roundFloat(val.Latest, 4),
		LastN:  roundSeriesTail(val.Series, 3),
	}
}

func buildStochSnapshot(val indicator.IndicatorValue) *stochSnapshot {
	if len(val.Series) == 0 {
		return nil
	}
	return &stochSnapshot{
		Current: roundFloat(val.Latest, 4),
		LastN:   roundSeriesTail(val.Series, 2),
		RangeLo: 0,
		RangeHi: 100,
	}
}

func buildATRSnapshot(val indicator.IndicatorValue) *atrSnapshot {
	if val.Latest == 0 && len(val.Series) == 0 {
		return nil
	}
	maxVal, minVal := seriesBounds(val.Series)
	return &atrSnapshot{
		Latest:  roundFloat(val.Latest, 4),
		LastN:   roundSeriesTail(val.Series, 3),
		RangeLo: roundFloat(minVal, 4),
		RangeHi: roundFloat(maxVal, 4),
	}
}

func roundSeriesTail(series []float64, n int) []float64 {
	if n <= 0 || len(series) == 0 {
		return nil
	}
	start := len(series) - n
	if start < 0 {
		start = 0
	}
	out := make([]float64, 0, len(series)-start)
	for i := start; i < len(series); i++ {
		out = append(out, roundFloat(series[i], 4))
	}
	return out
}

func seriesBounds(series []float64) (max, min float64) {
	if len(series) == 0 {
		return 0, 0
	}
	max = -math.MaxFloat64
	min = math.MaxFloat64
	for _, v := range series {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		if v > max {
			max = v
		}
		if v < min {
			min = v
		}
	}
	if max == -math.MaxFloat64 {
		max = 0
	}
	if min == math.MaxFloat64 {
		min = 0
	}
	return roundFloat(max, 4), roundFloat(min, 4)
}

func roundFloat(v float64, digits int) float64 {
	if digits <= 0 {
		return math.Round(v)
	}
	factor := math.Pow10(digits)
	return math.Round(v*factor) / factor
}

func sanitizeSeries(series []float64) []float64 {
	if len(series) == 0 {
		return nil
	}
	out := make([]float64, 0, len(series))
	for _, v := range series {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		out = append(out, roundFloat(v, 4))
	}
	return out
}

func candleTimestamp(c market.Candle) string {
	ts := c.CloseTime
	if ts == 0 {
		ts = c.OpenTime
	}
	if ts == 0 {
		return time.Now().UTC().Format(time.RFC3339)
	}
	return time.UnixMilli(ts).UTC().Format(time.RFC3339)
}
