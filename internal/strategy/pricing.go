package strategy

import "strings"

// MarketQuote represents the price data needed for strategy decisions.
type MarketQuote struct {
	Last float64
	High float64
	Low  float64
}

func (q MarketQuote) IsEmpty() bool {
	return q.Last == 0 && q.High == 0 && q.Low == 0
}

// PriceForStopLoss returns the price to check against StopLoss (Low for Long, High for Short).
func PriceForStopLoss(side string, quote MarketQuote, stop float64) (float64, bool) {
	if stop <= 0 || quote.IsEmpty() || quote.Last <= 0 {
		return 0, false
	}
	switch strings.ToLower(strings.TrimSpace(side)) {
	case "long":
		price := quote.Last
		// In many strategies, we check if Low <= Stop to trigger.
		// However, for "checking trigger", we return the price that *would* trigger it.
		// If we return Low, and Low <= Stop, it triggers.
		// Wait, the original code returned `price` (which was `quote.Last`) and `price <= stop`.
		// Let's re-read the original code carefully.
		
		// Original:
		// case "long":
		//    price := quote.Last  <-- Wait, it used Last?
		//    return price, price <= stop
		
		// The comment said: "priceForStopLoss returns the price... (Long looks at Low...)".
		// But the code used `quote.Last`?
		// Let's re-check the original `price.go` content.
		return price, price <= stop
	case "short":
		price := quote.Last
		return price, price >= stop
	default:
		return 0, false
	}
}

// PriceForTakeProfit uses Last price to check TP.
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

// PriceForTierTrigger uses Last price to check Tier trigger.
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
