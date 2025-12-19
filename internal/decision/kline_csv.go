package decision

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"brale/internal/market"
)

type CandleCSVOptions struct {
	Location       *time.Location
	PricePrecision int
	Interval       string
}

const (
	PrecisionAuto = math.MinInt32

	PrecisionRaw = -1
)

func BuildCandleCSV(candles []market.Candle, opts CandleCSVOptions) string {
	if len(candles) == 0 {
		return ""
	}
	loc := opts.Location
	if loc == nil {
		loc = time.UTC
	}
	precision := opts.PricePrecision
	if precision == PrecisionAuto {
		precision = autoPrecisionFromCandles(candles)
	}
	var b strings.Builder
	metaParts := []string{}
	start := firstTimestamp(candles)
	if start != 0 {
		metaParts = append(metaParts, fmt.Sprintf("WindowStart=%s", time.UnixMilli(start).In(loc).Format(time.RFC3339)))
	}
	if iv := strings.TrimSpace(opts.Interval); iv != "" {
		metaParts = append(metaParts, fmt.Sprintf("Interval=%s", strings.ToUpper(iv)))
	}
	metaParts = append(metaParts, "Order=OLDEST->NEWEST")
	b.WriteString("# " + strings.Join(metaParts, " ") + "\n")
	b.WriteString("Index,O,H,L,C,V\n")
	for idx, c := range candles {
		b.WriteString(strconv.Itoa(idx + 1))
		b.WriteByte(',')
		b.WriteString(formatPrice(c.Open, precision))
		b.WriteByte(',')
		b.WriteString(formatPrice(c.High, precision))
		b.WriteByte(',')
		b.WriteString(formatPrice(c.Low, precision))
		b.WriteByte(',')
		b.WriteString(formatPrice(c.Close, precision))
		b.WriteByte(',')
		b.WriteString(formatPlainFloat(c.Volume))
		b.WriteByte('\n')
	}
	return b.String()
}

func autoPrecisionFromCandles(candles []market.Candle) int {
	maxVal := 0.0
	for _, c := range candles {
		for _, v := range []float64{c.Open, c.High, c.Low, c.Close} {
			abs := math.Abs(v)
			if abs > maxVal {
				maxVal = abs
			}
		}
	}
	switch {
	case maxVal >= 1000:
		return 1
	case maxVal >= 100:
		return 2
	default:
		return PrecisionRaw
	}
}

func formatPrice(value float64, precision int) string {
	if precision == PrecisionRaw {
		return strconv.FormatFloat(value, 'f', -1, 64)
	}
	s := strconv.FormatFloat(value, 'f', precision, 64)
	if precision > 0 {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return s
}

func formatPlainFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func firstTimestamp(candles []market.Candle) int64 {
	if len(candles) == 0 {
		return 0
	}
	if ts := candles[0].OpenTime; ts != 0 {
		return ts
	}
	return candles[0].CloseTime
}
