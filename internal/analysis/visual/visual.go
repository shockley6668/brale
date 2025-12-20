package visual

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/go-echarts/go-echarts/v2/charts"
	"github.com/go-echarts/go-echarts/v2/components"
	"github.com/go-echarts/go-echarts/v2/opts"
	"github.com/go-echarts/go-echarts/v2/types"
	talib "github.com/markcheno/go-talib"

	"brale/internal/analysis/indicator"
	"brale/internal/analysis/pattern"
	"brale/internal/market"
)

type ImageResult struct {
	Bytes       []byte `json:"-"`
	Base64      string `json:"base64"`
	Filename    string `json:"filename"`
	Description string `json:"description"`
}

func (r *ImageResult) DataURI() string {
	if r == nil {
		return ""
	}
	if r.Base64 == "" && len(r.Bytes) > 0 {
		r.Base64 = base64.StdEncoding.EncodeToString(r.Bytes)
	}
	if r.Base64 == "" {
		return ""
	}
	return "data:image/png;base64," + r.Base64
}

type CompositeInput struct {
	Context    context.Context
	Symbol     string
	Horizon    string
	Intervals  []string
	Candles    map[string][]market.Candle
	History    map[string][]market.Candle
	Indicators map[string]indicator.Report
	Patterns   map[string]pattern.Result
}

const (
	colorBackground    = "#060c1b"
	colorTextPrimary   = "#eceff4"
	colorTextSecondary = "#9ca3af"
	colorBull          = "#34d399"
	colorBear          = "#f87171"
	colorEmaFast       = "#3b82f6"
	colorEmaMid        = "#fbbf24"
	colorEmaSlow       = "#f472b6"
	colorVolume        = "#a78bfa"
	colorDIF           = "#22d3ee"
	colorDEA           = "#fb7185"

	chartWidthPx   = 1600
	klineHeightPx  = 600
	volumeHeightPx = 260
	macdHeightPx   = 260
)

func RenderComposite(input CompositeInput) (ImageResult, error) {
	if err := EnsureHeadlessAvailable(input.Context); err != nil {
		return ImageResult{}, err
	}
	if input.Symbol == "" {
		return ImageResult{}, fmt.Errorf("symbol required for composite render")
	}
	if len(input.Intervals) == 0 {
		return ImageResult{}, fmt.Errorf("at least one interval required for %s", input.Symbol)
	}
	html, desc, err := buildCompositeHTML(input)
	if err != nil {
		return ImageResult{}, err
	}
	blockHeight := klineHeightPx + volumeHeightPx + macdHeightPx
	height := len(input.Intervals) * blockHeight
	if height < 520 {
		height = 520
	}
	png, err := renderHTMLToPNG(input.Context, html, chartWidthPx, height)
	if err != nil {
		return ImageResult{}, err
	}
	img := ImageResult{
		Bytes:       png,
		Base64:      base64.StdEncoding.EncodeToString(png),
		Filename:    fmt.Sprintf("%s_composite.png", strings.ToLower(input.Symbol)),
		Description: desc,
	}
	return img, nil
}

var (
	headlessOnce sync.Once
	headlessErr  error
)

func EnsureHeadlessAvailable(ctx context.Context) error {
	headlessOnce.Do(func() {
		targetCtx := ctx
		if targetCtx == nil {
			targetCtx = context.Background()
		}
		parent, cancel := chromedp.NewContext(targetCtx)
		if cancel != nil {
			defer cancel()
		}
		headlessErr = chromedp.Run(parent)
	})
	return headlessErr
}

func buildCompositeHTML(input CompositeInput) ([]byte, string, error) {
	page := components.NewPage()
	page.SetLayout(components.PageFlexLayout)
	descriptions := make([]string, 0, len(input.Intervals))

	for _, interval := range input.Intervals {
		candles := input.Candles[interval]
		if len(candles) == 0 {
			continue
		}
		history := input.History[interval]
		if len(history) == 0 {
			history = candles
		}
		info := buildIntervalSummary(interval, input.Patterns[interval], input.Indicators[interval])
		descriptions = append(descriptions, fmt.Sprintf("%s: %s", interval, info.Summary))

		minPrice, maxPrice := priceBounds(candles)
		padding := (maxPrice - minPrice) * 0.05
		if padding <= 0 {
			padding = math.Max(1, math.Abs(maxPrice)*0.01)
		}
		minAxis := round(minPrice-padding, 4)
		maxAxis := round(maxPrice+padding, 4)

		init := opts.Initialization{
			Theme:           types.ThemeWesteros,
			Width:           fmt.Sprintf("%dpx", chartWidthPx),
			Height:          fmt.Sprintf("%dpx", klineHeightPx),
			BackgroundColor: colorBackground,
		}
		kline := charts.NewKLine()
		kline.SetGlobalOptions(
			charts.WithInitializationOpts(init),
			charts.WithLegendOpts(opts.Legend{Show: opts.Bool(true), TextStyle: &opts.TextStyle{Color: colorTextPrimary}}),
			charts.WithTitleOpts(opts.Title{
				Title:         fmt.Sprintf("%s %s", strings.ToUpper(input.Symbol), interval),
				Subtitle:      info.Subtitle,
				Left:          "left",
				Top:           "10",
				TitleStyle:    &opts.TextStyle{Color: colorTextPrimary, FontSize: 18},
				SubtitleStyle: &opts.TextStyle{Color: colorTextSecondary},
			}),
			charts.WithTooltipOpts(opts.Tooltip{Show: opts.Bool(true), Trigger: "axis"}),
			charts.WithDataZoomOpts(opts.DataZoom{Type: "slider", XAxisIndex: []int{0}}),
			charts.WithXAxisOpts(opts.XAxis{
				Type:      "category",
				AxisLabel: &opts.AxisLabel{Color: colorTextSecondary},
				SplitLine: &opts.SplitLine{Show: opts.Bool(false)},
			}),
			charts.WithYAxisOpts(opts.YAxis{
				Scale:     opts.Bool(true),
				AxisLabel: &opts.AxisLabel{Color: colorTextSecondary},
				Min:       minAxis,
				Max:       maxAxis,
				SplitLine: &opts.SplitLine{Show: opts.Bool(true), LineStyle: &opts.LineStyle{Color: colorTextSecondary, Opacity: opts.Float(0.2)}},
			}),
		)
		kline.SetSeriesOptions(
			charts.WithItemStyleOpts(opts.ItemStyle{
				Color:        colorBull,
				Color0:       colorBear,
				BorderColor:  colorBull,
				BorderColor0: colorBear,
			}),
		)

		xAxis := buildXAxis(candles)
		klineData := buildKlineSeries(interval, candles)
		kline.SetXAxis(xAxis)
		kline.AddSeries(fmt.Sprintf("Price_%s", interval), klineData)

		emaLine := buildEMALine(interval, candles, input.Indicators[interval])
		emaLine.SetXAxis(xAxis)
		kline.Overlap(emaLine)

		volume := buildVolumeChart(interval, xAxis, candles)
		macdChart := buildMACDChart(interval, xAxis, candles, history)

		page.AddCharts(kline, volume, macdChart)
	}

	if len(page.Charts) == 0 {
		return nil, "", fmt.Errorf("no charts rendered for %s", input.Symbol)
	}

	var buf bytes.Buffer
	if err := page.Render(&buf); err != nil {
		return nil, "", err
	}
	sort.Strings(descriptions)
	desc := fmt.Sprintf("%s | %s", strings.ToUpper(input.Symbol), strings.Join(descriptions, " | "))
	return buf.Bytes(), desc, nil
}

func buildIntervalSummary(interval string, pat pattern.Result, rep indicator.Report) (info struct{ Summary, Subtitle string }) {
	summary := pat.PatternSummary
	if summary == "" {
		summary = "结构稳定"
	}
	trend := pat.TrendSummary
	if trend == "" {
		trend = "无趋势描述"
	}
	bias := pat.Bias
	if bias != "" {
		trend = fmt.Sprintf("%s | bias=%s", trend, bias)
	}
	rsi := rep.Values["rsi"].Latest
	macd := rep.Values["macd"].State
	info.Summary = fmt.Sprintf("%s / %s", summary, trend)
	info.Subtitle = fmt.Sprintf("RSI %.1f | MACD %s", rsi, macd)
	return
}

func buildXAxis(candles []market.Candle) []string {
	x := make([]string, len(candles))
	for i, c := range candles {
		x[i] = time.UnixMilli(c.CloseTime).UTC().Format("01-02 15:04")
	}
	return x
}

func buildKlineSeries(interval string, candles []market.Candle) []opts.KlineData {
	data := make([]opts.KlineData, 0, len(candles))
	for _, c := range candles {
		data = append(data, opts.KlineData{Value: [4]float64{c.Open, c.Close, c.Low, c.High}})
	}
	return data
}

func buildEMALine(interval string, candles []market.Candle, rep indicator.Report) *charts.Line {
	line := charts.NewLine()
	line.SetSeriesOptions(
		charts.WithLineChartOpts(opts.LineChart{ShowSymbol: opts.Bool(false)}),
	)
	fast := rep.Values["ema_fast"]
	mid := rep.Values["ema_mid"]
	slow := rep.Values["ema_slow"]
	emaFast := toLineData(fast.Series, len(candles))
	emaMid := toLineData(mid.Series, len(candles))
	emaSlow := toLineData(slow.Series, len(candles))
	line.AddSeries(emaLegendLabel(fast.Note, "EMA Fast"), emaFast, charts.WithLineStyleOpts(opts.LineStyle{Color: colorEmaFast, Width: 2}))
	line.AddSeries(emaLegendLabel(mid.Note, "EMA Mid"), emaMid, charts.WithLineStyleOpts(opts.LineStyle{Color: colorEmaMid, Width: 2}))
	line.AddSeries(emaLegendLabel(slow.Note, "EMA Slow"), emaSlow, charts.WithLineStyleOpts(opts.LineStyle{Color: colorEmaSlow, Width: 2}))
	return line
}

func emaLegendLabel(note, fallback string) string {
	note = strings.TrimSpace(note)
	if note != "" {
		fields := strings.Fields(note)
		if len(fields) > 0 && fields[0] != "" {
			return fields[0]
		}
	}
	return fallback
}

func buildVolumeChart(interval string, xAxis []string, candles []market.Candle) *charts.Bar {
	bar := charts.NewBar()
	bar.SetGlobalOptions(
		charts.WithInitializationOpts(opts.Initialization{
			Theme:           types.ThemeWesteros,
			Width:           fmt.Sprintf("%dpx", chartWidthPx),
			Height:          fmt.Sprintf("%dpx", volumeHeightPx),
			BackgroundColor: colorBackground,
		}),
		charts.WithTitleOpts(opts.Title{Title: fmt.Sprintf("Volume %s", interval), Left: "left", TitleStyle: &opts.TextStyle{Color: colorTextPrimary}}),
		charts.WithLegendOpts(opts.Legend{Show: opts.Bool(false)}),
		charts.WithTooltipOpts(opts.Tooltip{Show: opts.Bool(true), Trigger: "axis"}),
		charts.WithXAxisOpts(opts.XAxis{
			SplitNumber: 6,
			AxisLabel:   &opts.AxisLabel{Show: opts.Bool(false)},
		}),
		charts.WithYAxisOpts(opts.YAxis{
			AxisLabel: &opts.AxisLabel{Show: opts.Bool(true), Color: colorTextSecondary},
			SplitLine: &opts.SplitLine{Show: opts.Bool(true), LineStyle: &opts.LineStyle{Color: colorTextSecondary, Opacity: opts.Float(0.15)}},
		}),
	)
	vols := make([]opts.BarData, len(candles))
	for i, c := range candles {
		color := colorBear
		if c.Close >= c.Open {
			color = colorBull
		}
		vols[i] = opts.BarData{
			Value: c.Volume,
			ItemStyle: &opts.ItemStyle{
				Color:   color,
				Opacity: opts.Float(0.6),
			},
		}
	}
	bar.SetXAxis(xAxis)
	bar.AddSeries("Volume", vols)
	return bar
}

func buildMACDChart(interval string, xAxis []string, candles []market.Candle, history []market.Candle) *charts.Bar {
	bar := charts.NewBar()
	bar.SetGlobalOptions(
		charts.WithInitializationOpts(opts.Initialization{
			Theme:           types.ThemeWesteros,
			Width:           fmt.Sprintf("%dpx", chartWidthPx),
			Height:          fmt.Sprintf("%dpx", macdHeightPx),
			BackgroundColor: colorBackground,
		}),
		charts.WithTitleOpts(opts.Title{Title: fmt.Sprintf("MACD %s", interval), Left: "left", TitleStyle: &opts.TextStyle{Color: colorTextPrimary}}),
		charts.WithLegendOpts(opts.Legend{Show: opts.Bool(true), TextStyle: &opts.TextStyle{Color: colorTextSecondary}}),
		charts.WithTooltipOpts(opts.Tooltip{Show: opts.Bool(true), Trigger: "axis"}),
		charts.WithXAxisOpts(opts.XAxis{AxisLabel: &opts.AxisLabel{Show: opts.Bool(false)}}),
		charts.WithYAxisOpts(opts.YAxis{
			AxisLabel: &opts.AxisLabel{Show: opts.Bool(true), Color: colorTextSecondary},
			SplitLine: &opts.SplitLine{Show: opts.Bool(true), LineStyle: &opts.LineStyle{Color: colorTextSecondary, Opacity: opts.Float(0.15)}},
		}),
	)
	dif, dea, hist := calcMACDSeries(history)
	windowLen := len(candles)
	dif = tailSeries(dif, windowLen)
	dea = tailSeries(dea, windowLen)
	hist = tailSeries(hist, windowLen)
	histData := make([]opts.BarData, len(candles))
	for i, v := range hist {
		if math.IsNaN(v) {
			histData[i] = opts.BarData{Value: nil}
			continue
		}
		color := colorBear
		if v >= 0 {
			color = colorBull
		}
		histData[i] = opts.BarData{
			Value: round(v, 4),
			ItemStyle: &opts.ItemStyle{
				Color: color,
			},
		}
	}
	for i := len(hist); i < len(candles); i++ {
		histData[i] = opts.BarData{Value: nil}
	}
	bar.SetXAxis(xAxis)
	bar.AddSeries("MACD Hist", histData)

	line := charts.NewLine()
	line.SetSeriesOptions(
		charts.WithLineStyleOpts(opts.LineStyle{Width: 2}),
		charts.WithLineChartOpts(opts.LineChart{ShowSymbol: opts.Bool(false)}),
	)
	line.SetXAxis(xAxis)
	line.AddSeries("DIF", toLineData(dif, len(candles)), charts.WithLineStyleOpts(opts.LineStyle{Color: colorDIF, Width: 2}))
	line.AddSeries("DEA", toLineData(dea, len(candles)), charts.WithLineStyleOpts(opts.LineStyle{Color: colorDEA, Width: 2}))
	bar.Overlap(line)
	return bar
}

func toLineData(series []float64, length int) []opts.LineData {
	line := make([]opts.LineData, length)
	offset := length - len(series)
	if offset < 0 {
		offset = 0
	}
	for i := 0; i < offset; i++ {
		line[i] = opts.LineData{Value: nil}
	}
	for i := 0; i < len(series) && offset+i < length; i++ {
		val := series[i]
		if math.IsNaN(val) {
			line[offset+i] = opts.LineData{Value: nil}
		} else {
			line[offset+i] = opts.LineData{Value: round(val, 4)}
		}
	}
	return line
}

func tailSeries(series []float64, keep int) []float64 {
	if keep <= 0 || len(series) == 0 {
		return nil
	}
	if len(series) <= keep {
		return series
	}
	return series[len(series)-keep:]
}

func calcMACDSeries(candles []market.Candle) (dif, dea, hist []float64) {
	const slow = 26
	if len(candles) < slow {
		return nil, nil, nil
	}
	closes := make([]float64, len(candles))
	for i, c := range candles {
		closes[i] = c.Close
	}
	dif, dea, hist = talib.Macd(closes, 12, 26, 9)
	return dif, dea, hist
}

func round(val float64, decimals int) float64 {
	if decimals <= 0 {
		return math.Round(val)
	}
	scale := math.Pow10(decimals)
	return math.Round(val*scale) / scale
}

func priceBounds(candles []market.Candle) (minVal, maxVal float64) {
	if len(candles) == 0 {
		return 0, 0
	}
	minVal = candles[0].Low
	maxVal = candles[0].High
	for _, c := range candles {
		if c.Low < minVal {
			minVal = c.Low
		}
		if c.High > maxVal {
			maxVal = c.High
		}
	}
	return minVal, maxVal
}

func renderHTMLToPNG(ctx context.Context, html []byte, width, height int) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	parent, cancel := chromedp.NewContext(ctx)
	defer cancel()

	timeoutCtx, cancelTimeout := context.WithTimeout(parent, 20*time.Second)
	defer cancelTimeout()

	dataURI := "data:text/html;base64," + base64.StdEncoding.EncodeToString(html)
	var screenshot []byte
	tasks := chromedp.Tasks{
		chromedp.EmulateViewport(int64(width), int64(height)),
		chromedp.Navigate(dataURI),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Sleep(1500 * time.Millisecond),
		chromedp.FullScreenshot(&screenshot, 0),
	}
	if err := chromedp.Run(timeoutCtx, tasks...); err != nil {
		return nil, err
	}
	return screenshot, nil
}
