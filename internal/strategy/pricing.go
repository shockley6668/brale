package strategy

import "strings"

type MarketQuote struct {
	Last float64
	High float64
	Low  float64
}

func (q MarketQuote) IsEmpty() bool {
	return q.Last == 0 && q.High == 0 && q.Low == 0
}

func PriceForStopLoss(side string, quote MarketQuote, stop float64) (float64, bool) {
	if stop <= 0 || quote.IsEmpty() || quote.Last <= 0 {
		return 0, false
	}
	switch strings.ToLower(strings.TrimSpace(side)) {
	case "long":
		price := quote.Last

		return price, price <= stop
	case "short":
		price := quote.Last
		return price, price >= stop
	default:
		return 0, false
	}
}

func PriceForTakeProfit(side string, quote MarketQuote, tp float64) (float64, bool) {
	if tp <= 0 || quote.IsEmpty() || quote.Last <= 0 {
		return 0, false
	}
	switch strings.ToLower(strings.TrimSpace(side)) {
	case "long":
		price := quote.Last
		return price, price >= tp
	case "short":
		price := quote.Last
		return price, price <= tp
	default:
		return 0, false
	}
}

func PriceForTierTrigger(side string, quote MarketQuote, target float64) (float64, bool) {
	if target <= 0 || quote.IsEmpty() || quote.Last <= 0 {
		return 0, false
	}
	switch strings.ToLower(strings.TrimSpace(side)) {
	case "long":
		price := quote.Last
		return price, price >= target
	case "short":
		price := quote.Last
		return price, price <= target
	default:
		return 0, false
	}
}
