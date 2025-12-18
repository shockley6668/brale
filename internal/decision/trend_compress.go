package decision

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"brale/internal/market"

	"github.com/markcheno/go-talib"
)

type TrendCompressOptions struct {
	FractalSpan         int
	MaxStructurePoints  int
	DedupDistanceBars   int
	DedupATRFactor      float64
	RSIPeriod           int
	ATRPeriod           int
	RecentCandles       int
	VolumeMAPeriod      int
	EMA20Period         int
	EMA50Period         int
	EMA200Period        int
	Pretty              bool
	IncludeCurrentRSI   bool
	IncludeStructureRSI bool
}

func DefaultTrendCompressOptions() TrendCompressOptions {
	return TrendCompressOptions{
		FractalSpan:         2, // 5-bar fractal
		MaxStructurePoints:  8,
		DedupDistanceBars:   10,
		DedupATRFactor:      0.5,
		RSIPeriod:           14,
		ATRPeriod:           14,
		RecentCandles:       7,
		VolumeMAPeriod:      20,
		EMA20Period:         20,
		EMA50Period:         50,
		EMA200Period:        200,
		Pretty:              false,
		IncludeCurrentRSI:   true,
		IncludeStructureRSI: true,
	}
}

type TrendCompressedInput struct {
	Meta            TrendCompressedMeta      `json:"meta"`
	StructurePoints []TrendStructurePoint    `json:"structure_points"`
	RecentCandles   []TrendRecentCandle      `json:"recent_candles"`
	GlobalContext   TrendGlobalContext       `json:"global_context"`
	RawCandles      []TrendRawCandleOptional `json:"raw_candles,omitempty"`
}

type TrendCompressedMeta struct {
	Symbol    string `json:"symbol"`
	Interval  string `json:"interval"`
	Timestamp string `json:"timestamp"`
}

type TrendStructurePoint struct {
	Idx   int      `json:"idx"`
	Type  string   `json:"type"` // "High" | "Low"
	Price float64  `json:"price"`
	RSI   *float64 `json:"rsi,omitempty"`
}

type TrendRecentCandle struct {
	Idx int      `json:"idx"`
	O   float64  `json:"o"`
	H   float64  `json:"h"`
	L   float64  `json:"l"`
	C   float64  `json:"c"`
	V   float64  `json:"v"`
	RSI *float64 `json:"rsi,omitempty"`
}

type TrendGlobalContext struct {
	TrendSlope float64  `json:"trend_slope"`
	VolRatio   float64  `json:"vol_ratio"`
	EMA20      *float64 `json:"ema20,omitempty"`
	EMA50      *float64 `json:"ema50,omitempty"`
	EMA200     *float64 `json:"ema200,omitempty"`
}

type TrendRawCandleOptional struct {
	Idx int     `json:"idx"`
	O   float64 `json:"o"`
	H   float64 `json:"h"`
	L   float64 `json:"l"`
	C   float64 `json:"c"`
	V   float64 `json:"v"`
}

func BuildTrendCompressedJSON(symbol, interval string, candles []market.Candle, opts TrendCompressOptions) (string, error) {
	payload, err := BuildTrendCompressedInput(symbol, interval, candles, opts)
	if err != nil {
		return "", err
	}
	var out []byte
	if opts.Pretty {
		out, err = json.MarshalIndent(payload, "", "  ")
	} else {
		out, err = json.Marshal(payload)
	}
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func BuildTrendCompressedInput(symbol, interval string, candles []market.Candle, opts TrendCompressOptions) (TrendCompressedInput, error) {
	if len(candles) == 0 {
		return TrendCompressedInput{}, fmt.Errorf("no candles")
	}
	opts = normalizeTrendCompressOptions(opts)
	n := len(candles)

	closes := make([]float64, n)
	highs := make([]float64, n)
	lows := make([]float64, n)
	volumes := make([]float64, n)
	for i, c := range candles {
		closes[i] = c.Close
		highs[i] = c.High
		lows[i] = c.Low
		volumes[i] = c.Volume
	}

	rsiSeries := talib.Rsi(closes, opts.RSIPeriod)
	atrSeries := talib.Atr(highs, lows, closes, opts.ATRPeriod)

	meta := TrendCompressedMeta{
		Symbol:    strings.TrimSpace(symbol),
		Interval:  strings.TrimSpace(interval),
		Timestamp: candleTimestamp(candles[n-1]),
	}

	gc := TrendGlobalContext{
		TrendSlope: roundFloat(linRegSlope(closes), 4),
		VolRatio:   roundFloat(volumeRatio(volumes, opts.VolumeMAPeriod), 3),
	}
	if v := lastNonZero(talib.Ema(closes, opts.EMA20Period)); v > 0 {
		v = roundFloat(v, 4)
		gc.EMA20 = &v
	}
	if v := lastNonZero(talib.Ema(closes, opts.EMA50Period)); v > 0 {
		v = roundFloat(v, 4)
		gc.EMA50 = &v
	}
	if v := lastNonZero(talib.Ema(closes, opts.EMA200Period)); v > 0 {
		v = roundFloat(v, 4)
		gc.EMA200 = &v
	}

	structurePoints := selectStructurePoints(candles, highs, lows, rsiSeries, atrSeries, opts)
	recentCandles := buildRecentCandles(candles, rsiSeries, opts)

	return TrendCompressedInput{
		Meta:            meta,
		StructurePoints: structurePoints,
		RecentCandles:   recentCandles,
		GlobalContext:   gc,
	}, nil
}

func normalizeTrendCompressOptions(opts TrendCompressOptions) TrendCompressOptions {
	def := DefaultTrendCompressOptions()
	if opts.FractalSpan <= 0 {
		opts.FractalSpan = def.FractalSpan
	}
	if opts.MaxStructurePoints <= 0 {
		opts.MaxStructurePoints = def.MaxStructurePoints
	}
	if opts.DedupDistanceBars <= 0 {
		opts.DedupDistanceBars = def.DedupDistanceBars
	}
	if opts.DedupATRFactor <= 0 {
		opts.DedupATRFactor = def.DedupATRFactor
	}
	if opts.RSIPeriod <= 0 {
		opts.RSIPeriod = def.RSIPeriod
	}
	if opts.ATRPeriod <= 0 {
		opts.ATRPeriod = def.ATRPeriod
	}
	if opts.RecentCandles <= 0 {
		opts.RecentCandles = def.RecentCandles
	}
	if opts.VolumeMAPeriod <= 0 {
		opts.VolumeMAPeriod = def.VolumeMAPeriod
	}
	if opts.EMA20Period <= 0 {
		opts.EMA20Period = def.EMA20Period
	}
	if opts.EMA50Period <= 0 {
		opts.EMA50Period = def.EMA50Period
	}
	if opts.EMA200Period <= 0 {
		opts.EMA200Period = def.EMA200Period
	}
	if !opts.IncludeCurrentRSI && !opts.IncludeStructureRSI {
		opts.IncludeCurrentRSI = def.IncludeCurrentRSI
		opts.IncludeStructureRSI = def.IncludeStructureRSI
	}
	return opts
}

func buildRecentCandles(candles []market.Candle, rsi []float64, opts TrendCompressOptions) []TrendRecentCandle {
	n := len(candles)
	keep := opts.RecentCandles
	if keep > n {
		keep = n
	}
	start := n - keep
	out := make([]TrendRecentCandle, 0, keep)
	for idx := start; idx < n; idx++ {
		c := candles[idx]
		rc := TrendRecentCandle{
			Idx: idx,
			O:   roundFloat(c.Open, 4),
			H:   roundFloat(c.High, 4),
			L:   roundFloat(c.Low, 4),
			C:   roundFloat(c.Close, 4),
			V:   roundFloat(c.Volume, 4),
		}
		if opts.IncludeCurrentRSI && idx == n-1 && idx < len(rsi) {
			v := roundFloat(rsi[idx], 1)
			rc.RSI = &v
		}
		out = append(out, rc)
	}
	return out
}

func selectStructurePoints(candles []market.Candle, highs, lows, rsi, atr []float64, opts TrendCompressOptions) []TrendStructurePoint {
	n := len(candles)
	span := opts.FractalSpan
	if n < span*2+1 {
		return nil
	}
	selected := make([]TrendStructurePoint, 0, opts.MaxStructurePoints)
	for idx := n - span - 1; idx >= span; idx-- {
		if isFractalHigh(highs, idx, span) {
			p := TrendStructurePoint{Idx: idx, Type: "High", Price: roundFloat(highs[idx], 4)}
			if opts.IncludeStructureRSI && idx < len(rsi) {
				v := roundFloat(rsi[idx], 1)
				p.RSI = &v
			}
			selected = mergeStructurePoint(selected, p, atr, opts)
		}
		if isFractalLow(lows, idx, span) {
			p := TrendStructurePoint{Idx: idx, Type: "Low", Price: roundFloat(lows[idx], 4)}
			if opts.IncludeStructureRSI && idx < len(rsi) {
				v := roundFloat(rsi[idx], 1)
				p.RSI = &v
			}
			selected = mergeStructurePoint(selected, p, atr, opts)
		}
		if len(selected) >= opts.MaxStructurePoints {
			continue
		}
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].Idx < selected[j].Idx })
	return selected
}

func mergeStructurePoint(existing []TrendStructurePoint, candidate TrendStructurePoint, atr []float64, opts TrendCompressOptions) []TrendStructurePoint {
	for i := range existing {
		other := existing[i]
		if other.Type != candidate.Type {
			continue
		}
		distance := absInt(other.Idx - candidate.Idx)
		if distance >= opts.DedupDistanceBars {
			continue
		}
		threshold := 0.0
		if candidate.Idx >= 0 && candidate.Idx < len(atr) {
			threshold = atr[candidate.Idx] * opts.DedupATRFactor
		}
		if threshold <= 0 && other.Idx >= 0 && other.Idx < len(atr) {
			threshold = atr[other.Idx] * opts.DedupATRFactor
		}
		if threshold <= 0 {
			continue
		}
		if math.Abs(other.Price-candidate.Price) >= threshold {
			continue
		}
		if candidate.Type == "High" {
			if candidate.Price > other.Price {
				existing[i] = candidate
			}
		} else if candidate.Type == "Low" {
			if candidate.Price < other.Price {
				existing[i] = candidate
			}
		}
		return existing
	}
	if len(existing) >= opts.MaxStructurePoints {
		return existing
	}
	return append(existing, candidate)
}

func isFractalHigh(highs []float64, idx, span int) bool {
	v := highs[idx]
	for i := 1; i <= span; i++ {
		if v <= highs[idx-i] || v <= highs[idx+i] {
			return false
		}
	}
	return true
}

func isFractalLow(lows []float64, idx, span int) bool {
	v := lows[idx]
	for i := 1; i <= span; i++ {
		if v >= lows[idx-i] || v >= lows[idx+i] {
			return false
		}
	}
	return true
}

func linRegSlope(series []float64) float64 {
	n := len(series)
	if n == 0 {
		return 0
	}
	var sumX, sumY, sumXY, sumXX float64
	fn := float64(n)
	for i, y := range series {
		x := float64(i)
		sumX += x
		sumY += y
		sumXY += x * y
		sumXX += x * x
	}
	denom := fn*sumXX - sumX*sumX
	if denom == 0 {
		return 0
	}
	return (fn*sumXY - sumX*sumY) / denom
}

func volumeRatio(volumes []float64, lookback int) float64 {
	n := len(volumes)
	if n == 0 {
		return 0
	}
	if lookback <= 0 {
		lookback = 20
	}
	last := volumes[n-1]
	if n < 2 {
		return 0
	}
	count := lookback
	if count > n-1 {
		count = n - 1
	}
	if count <= 0 {
		return 0
	}
	sum := 0.0
	for i := n - 1 - count; i < n-1; i++ {
		sum += volumes[i]
	}
	avg := sum / float64(count)
	if avg == 0 {
		return 0
	}
	return last / avg
}

func lastNonZero(series []float64) float64 {
	for i := len(series) - 1; i >= 0; i-- {
		v := series[i]
		if math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		if math.Abs(v) <= 1e-12 {
			continue
		}
		return v
	}
	return 0
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
