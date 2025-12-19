package decision

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"brale/internal/analysis/indicator"
	"brale/internal/analysis/pattern"
	"brale/internal/analysis/visual"
	"brale/internal/logger"
	"brale/internal/market"
	"brale/internal/scheduler"
	"brale/internal/store"
)

type AnalysisContext struct {
	Symbol          string `json:"symbol"`
	Interval        string `json:"interval"`
	KlineJSON       string `json:"kline_json"`
	KlineCSV        string `json:"kline_csv"`
	IndicatorJSON   string `json:"indicator_json"`
	PatternReport   string `json:"pattern_report"`
	TrendReport     string `json:"trend_report"`
	ImageB64        string `json:"image_base64"`
	ImageNote       string `json:"image_note"`
	ForecastHorizon string `json:"forecast_horizon"`
}

type AnalysisBuildInput struct {
	Context           context.Context
	Exporter          store.SnapshotExporter
	Symbols           []string
	Intervals         []string
	Limit             int
	SliceLength       int
	SliceDrop         int
	HorizonName       string
	IndicatorLookback int
	WithImages        bool
	DisableIndicators bool
	RequireATR        bool
}

const defaultIndicatorLookback = 240

func BuildAnalysisContexts(input AnalysisBuildInput) []AnalysisContext {
	cfg, ok := normalizeAnalysisBuildInput(input)
	if !ok {
		return nil
	}
	result := make([]AnalysisContext, 0, len(cfg.symbols)*len(cfg.intervals))
	for _, rawSym := range cfg.symbols {
		result = append(result, buildAnalysisContextsForSymbol(cfg, rawSym)...)
	}
	return result
}

type analysisBuildConfig struct {
	ctx               context.Context
	exporter          store.SnapshotExporter
	symbols           []string
	intervals         []string
	limit             int
	sliceLen          int
	sliceDrop         int
	horizonName       string
	indicatorLookback int
	withImages        bool
	disableIndicators bool
	requireATR        bool
}

func normalizeAnalysisBuildInput(input AnalysisBuildInput) (analysisBuildConfig, bool) {
	if input.Exporter == nil {
		return analysisBuildConfig{}, false
	}
	ctx := input.Context
	if ctx == nil {
		ctx = context.Background()
	}
	limit := input.Limit
	if limit <= 0 {
		limit = 240
	}
	intervals := input.Intervals
	if len(intervals) == 0 {
		intervals = []string{"1h"}
	}
	indicatorLookback := input.IndicatorLookback
	if indicatorLookback <= 0 {
		indicatorLookback = defaultIndicatorLookback
	}
	return analysisBuildConfig{
		ctx:               ctx,
		exporter:          input.Exporter,
		symbols:           input.Symbols,
		intervals:         intervals,
		limit:             limit,
		sliceLen:          input.SliceLength,
		sliceDrop:         input.SliceDrop,
		horizonName:       input.HorizonName,
		indicatorLookback: indicatorLookback,
		withImages:        input.WithImages,
		disableIndicators: input.DisableIndicators,
		requireATR:        input.RequireATR,
	}, true
}

func buildAnalysisContextsForSymbol(cfg analysisBuildConfig, rawSym string) []AnalysisContext {
	sym := strings.ToUpper(strings.TrimSpace(rawSym))
	if sym == "" {
		return nil
	}
	out := make([]AnalysisContext, 0, len(cfg.intervals))
	for _, rawIv := range cfg.intervals {
		ac, ok := buildAnalysisContextForSymbolInterval(cfg, sym, rawIv)
		if !ok {
			continue
		}
		out = append(out, ac)
	}
	return out
}

func buildAnalysisContextForSymbolInterval(cfg analysisBuildConfig, sym string, rawIv string) (AnalysisContext, bool) {
	iv := strings.TrimSpace(rawIv)
	if iv == "" {
		return AnalysisContext{}, false
	}
	effectiveSlice := enforceIntervalSliceLimit(iv, cfg.sliceLen)
	candles := exportCandles(cfg, sym, iv)
	if len(candles) == 0 {
		return AnalysisContext{}, false
	}

	fullCandles, shortCandles, sourceCandles := prepareCandles(sym, iv, candles, effectiveSlice, cfg.sliceLen, cfg.sliceDrop)
	rawJSON, ok := marshalCandlesJSON(sym, iv, sourceCandles)
	if !ok {
		return AnalysisContext{}, false
	}
	csvData := buildCandleCSVData(shortCandles, iv)

	indJSON, rep, calculated, indErr := buildIndicatorPayload(cfg, sym, iv, fullCandles, shortCandles)
	if indErr != nil && calculated {
		logger.Warnf("indicator compute 失败 %s %s: %v", sym, iv, indErr)
	}

	pat := pattern.Analyze(shortCandles)
	trendReport := formatTrendReport(pat)

	ac := AnalysisContext{
		Symbol:          sym,
		Interval:        iv,
		KlineJSON:       rawJSON,
		KlineCSV:        csvData,
		IndicatorJSON:   indJSON,
		PatternReport:   pat.PatternSummary,
		TrendReport:     trendReport,
		ForecastHorizon: cfg.horizonName,
	}
	if cfg.withImages && calculated && indErr == nil {
		ac.ImageB64, ac.ImageNote = renderComposite(cfg.ctx, sym, iv, cfg.horizonName, shortCandles, rep, pat)
	}
	return ac, true
}

func exportCandles(cfg analysisBuildConfig, sym string, iv string) []market.Candle {
	candles, err := cfg.exporter.Export(cfg.ctx, sym, iv, cfg.limit)
	if err != nil {
		logger.Debugf("analysis/export 失败 %s %s: %v", sym, iv, err)
		return nil
	}
	if len(candles) == 0 {
		return nil
	}
	if dur, ok := scheduler.ParseIntervalDuration(iv); ok {
		candles = scheduler.DropUnclosedBinanceKline(candles, dur)
	}
	return candles
}

func prepareCandles(sym, iv string, candles []market.Candle, effectiveSlice, sliceLen, sliceDrop int) ([]market.Candle, []market.Candle, []market.Candle) {
	fullCandles := cloneRoundedCandles(candles)
	shortCandles := trimCandlesWindow(fullCandles, effectiveSlice, sliceDrop)
	if len(shortCandles) == 0 {
		logger.Debugf("analysis trim %s %s 结果为空，保留原始 %d 根", sym, iv, len(fullCandles))
		shortCandles = fullCandles
	} else if len(shortCandles) != len(fullCandles) {
		logger.Debugf("analysis trim %s %s: %d -> %d (slice=%d drop=%d)", sym, iv, len(fullCandles), len(shortCandles), sliceLen, sliceDrop)
	}

	sourceCandles := trimCandlesWindow(fullCandles, 0, sliceDrop)
	if len(sourceCandles) == 0 {
		sourceCandles = fullCandles
	}
	return fullCandles, shortCandles, sourceCandles
}

func marshalCandlesJSON(sym, iv string, candles []market.Candle) (string, bool) {
	rawJSON, err := json.Marshal(candles)
	if err != nil {
		logger.Warnf("analysis/json 序列化失败 %s %s: %v", sym, iv, err)
		return "", false
	}
	return string(rawJSON), true
}

func buildCandleCSVData(shortCandles []market.Candle, iv string) string {
	if len(shortCandles) == 0 {
		return ""
	}
	return BuildCandleCSV(shortCandles, CandleCSVOptions{
		Location:       time.UTC,
		PricePrecision: PrecisionAuto,
		Interval:       iv,
	})
}

func buildIndicatorPayload(cfg analysisBuildConfig, sym, iv string, fullCandles, shortCandles []market.Candle) (string, indicator.Report, bool, error) {
	rep, calculated, err := computeIndicators(cfg, sym, iv, fullCandles)
	if err != nil || !calculated {
		return "", rep, calculated, err
	}

	indJSON := ""
	if payload, snapErr := BuildIndicatorSnapshot(fullCandles, rep); snapErr == nil {
		indJSON = string(payload)
	} else {
		logger.Warnf("indicator snapshot 构建失败 %s %s: %v", sym, iv, snapErr)
	}
	if len(shortCandles) > 0 && len(shortCandles) < len(fullCandles) {
		rep = clipIndicatorReport(rep, len(shortCandles))
	}
	return indJSON, rep, calculated, err
}

func computeIndicators(cfg analysisBuildConfig, sym, iv string, fullCandles []market.Candle) (indicator.Report, bool, error) {
	switch {
	case !cfg.disableIndicators:
		if len(fullCandles) < cfg.indicatorLookback {
			err := fmt.Errorf("insufficient history: need %d got %d", cfg.indicatorLookback, len(fullCandles))
			logger.Debugf("analysis %s %s 指标历史不足，需要 %d 根，当前仅 %d 根", sym, iv, cfg.indicatorLookback, len(fullCandles))
			return indicator.Report{}, true, err
		}
		rep, err := indicator.ComputeAll(fullCandles, indicator.Settings{Symbol: sym, Interval: iv})
		return rep, true, err
	case cfg.requireATR:
		series, err := indicator.ComputeATRSeries(fullCandles, 14)
		if err != nil {
			return indicator.Report{}, true, err
		}
		rep := indicator.Report{
			Symbol:   sym,
			Interval: iv,
			Count:    len(fullCandles),
			Values: map[string]indicator.IndicatorValue{
				"atr": {
					Latest: series[len(series)-1],
					Series: series,
					Note:   "period=14",
					State:  "volatility",
				},
			},
		}
		return rep, true, nil
	default:
		return indicator.Report{}, false, nil
	}
}

func formatTrendReport(pat pattern.Result) string {
	trendReport := pat.TrendSummary
	if pat.Bias != "" {
		trendReport = fmt.Sprintf("%s | bias=%s", pat.TrendSummary, pat.Bias)
	}
	return trendReport
}

func renderComposite(ctx context.Context, sym, iv, horizon string, candles []market.Candle, rep indicator.Report, pat pattern.Result) (string, string) {
	imgInput := visual.CompositeInput{
		Context:    ctx,
		Symbol:     sym,
		Horizon:    horizon,
		Intervals:  []string{iv},
		Candles:    map[string][]market.Candle{iv: candles},
		Indicators: map[string]indicator.Report{iv: rep},
		Patterns:   map[string]pattern.Result{iv: pat},
	}
	img, err := visual.RenderComposite(imgInput)
	if err != nil {
		logger.Warnf("interval render 失败 %s %s: %v", sym, iv, err)
		return "", ""
	}
	return img.DataURI(), img.Description
}

func trimCandlesWindow(candles []market.Candle, sliceLen, dropTail int) []market.Candle {
	n := len(candles)
	if n == 0 || (sliceLen <= 0 && dropTail <= 0) {
		return candles
	}
	end := n
	if dropTail > 0 {
		if dropTail >= end {
			dropTail = end - 1
			if dropTail < 0 {
				dropTail = 0
			}
		}
		end -= dropTail
	}
	if end <= 0 {
		return nil
	}
	start := 0
	if sliceLen > 0 && sliceLen < end {
		start = end - sliceLen
	}
	return candles[start:end]
}

var intervalSliceLimits = map[string]int{
	"15m": 32,
	"30m": 32,
	"1h":  48,
	"2h":  48,
	"4h":  36,
	"1d":  30,
}

func enforceIntervalSliceLimit(interval string, requested int) int {
	limit, ok := intervalSliceLimits[strings.ToLower(strings.TrimSpace(interval))]
	if !ok {
		if requested <= 0 {
			return requested
		}
		return requested
	}
	if requested > 0 && requested < limit {
		return requested
	}
	return limit
}

func clipIndicatorReport(rep indicator.Report, keep int) indicator.Report {
	if keep <= 0 || keep >= rep.Count {
		return rep
	}
	out := rep
	out.Count = keep
	values := make(map[string]indicator.IndicatorValue, len(rep.Values))
	for k, v := range rep.Values {
		v.Series = trimFloatSeries(v.Series, keep)
		values[k] = v
	}
	out.Values = values
	return out
}

func trimFloatSeries(series []float64, keep int) []float64 {
	if keep <= 0 || len(series) == 0 {
		return nil
	}
	if len(series) <= keep {
		return append([]float64(nil), series...)
	}
	start := len(series) - keep
	return append([]float64(nil), series[start:]...)
}

func cloneRoundedCandles(src []market.Candle) []market.Candle {
	if len(src) == 0 {
		return nil
	}
	out := make([]market.Candle, len(src))
	for i, c := range src {
		out[i] = roundCandleValues(c)
	}
	return out
}

func roundCandleValues(c market.Candle) market.Candle {
	c.Open = roundTo(c.Open, 4)
	c.High = roundTo(c.High, 4)
	c.Low = roundTo(c.Low, 4)
	c.Close = roundTo(c.Close, 4)
	c.Volume = roundTo(c.Volume, 2)
	return c
}

func roundTo(v float64, decimals int) float64 {
	if decimals <= 0 {
		return math.Round(v)
	}
	pow := math.Pow(10, float64(decimals))
	return math.Round(v*pow) / pow
}
