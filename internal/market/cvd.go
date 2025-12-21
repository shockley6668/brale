package market

import "github.com/shopspring/decimal"

type CVDMetrics struct {
	Value      decimal.Decimal
	Momentum   decimal.Decimal
	Normalized decimal.Decimal
	Divergence string
	PeakFlip   string
}

func ComputeCVD(candles []Candle) (CVDMetrics, bool) {
	if len(candles) == 0 {
		return CVDMetrics{}, false
	}
	cvd := make([]decimal.Decimal, 0, len(candles))
	closes := make([]decimal.Decimal, 0, len(candles))
	cumulative := decimal.Zero
	for _, c := range candles {
		buy := decimal.NewFromFloat(c.TakerBuyVolume)
		sell := decimal.NewFromFloat(c.TakerSellVolume)
		cumulative = cumulative.Add(buy.Sub(sell))
		cvd = append(cvd, cumulative)
		closes = append(closes, decimal.NewFromFloat(c.Close))
	}

	last := cvd[len(cvd)-1]
	momentum := decimal.Zero
	if len(cvd) > 6 {
		momentum = last.Sub(cvd[len(cvd)-6])
	}

	minVal := cvd[0]
	maxVal := cvd[0]
	for _, v := range cvd[1:] {
		if v.LessThan(minVal) {
			minVal = v
		}
		if v.GreaterThan(maxVal) {
			maxVal = v
		}
	}

	norm := decimal.NewFromFloat(0.5)
	if maxVal.GreaterThan(minVal) {
		norm = last.Sub(minVal).Div(maxVal.Sub(minVal))
	}

	priceNow := closes[len(closes)-1]
	pricePrev := closes[0]
	cvdPrev := cvd[0]
	if len(closes) > 6 {
		pricePrev = closes[len(closes)-6]
		cvdPrev = cvd[len(cvd)-6]
	}

	divergence := "neutral"
	if priceNow.GreaterThan(pricePrev) && last.LessThan(cvdPrev) {
		divergence = "bearish"
	} else if priceNow.LessThan(pricePrev) && last.GreaterThan(cvdPrev) {
		divergence = "bullish"
	}

	peakFlip := "none"
	if len(cvd) > 3 {
		a := cvd[len(cvd)-1]
		b := cvd[len(cvd)-2]
		c := cvd[len(cvd)-3]
		if a.LessThan(b) && b.GreaterThan(c) {
			peakFlip = "top"
		} else if a.GreaterThan(b) && b.LessThan(c) {
			peakFlip = "bottom"
		}
	}

	return CVDMetrics{
		Value:      last,
		Momentum:   momentum,
		Normalized: norm,
		Divergence: divergence,
		PeakFlip:   peakFlip,
	}, true
}
