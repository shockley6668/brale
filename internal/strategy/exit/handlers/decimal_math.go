package handlers

import (
	"math"

	"github.com/shopspring/decimal"
)

var (
	decOne      = decimal.NewFromInt(1)
	decimalEps  = decimal.NewFromFloat(1e-8)
	decimalZero = decimal.Zero
)

func decFromFloat(val float64) decimal.Decimal {
	if math.IsNaN(val) || math.IsInf(val, 0) {
		return decimalZero
	}
	return decimal.NewFromFloat(val)
}

func decToFloat(val decimal.Decimal) float64 {
	f, _ := val.Float64()
	return f
}

func decimalCompare(a, b float64) int {
	return decFromFloat(a).Cmp(decFromFloat(b))
}

func decimalLTE(a, b float64) bool { return decimalCompare(a, b) <= 0 }
func decimalGTE(a, b float64) bool { return decimalCompare(a, b) >= 0 }
func decimalLT(a, b float64) bool  { return decimalCompare(a, b) < 0 }
func decimalGT(a, b float64) bool  { return decimalCompare(a, b) > 0 }

func relativeTarget(entry, pct float64, side string) float64 {
	if entry <= 0 || side == "" {
		return 0
	}
	base := decFromFloat(entry)
	pctDec := decFromFloat(pct)
	var factor decimal.Decimal
	switch side {
	case "short":
		factor = decOne.Sub(pctDec)
	default:
		factor = decOne.Add(pctDec)
	}
	return decToFloat(base.Mul(factor))
}

func relativePrice(entry, pct float64, side string) float64 {
	if entry <= 0 {
		return 0
	}
	return relativeTarget(entry, pct, side)
}

func tierTargetHit(side string, price, target float64) bool {
	if price <= 0 || target <= 0 {
		return false
	}
	switch side {
	case "short":
		return decimalLTE(price, target)
	default:
		return decimalGTE(price, target)
	}
}

func hitStopLoss(side string, price, target float64) bool {
	if target <= 0 || price <= 0 {
		return false
	}
	switch side {
	case "short":
		return decimalGTE(price, target)
	default:
		return decimalLTE(price, target)
	}
}

func activationHit(side string, price, activation float64) bool {
	if price <= 0 || activation <= 0 {
		return false
	}
	switch side {
	case "short":
		return decimalLTE(price, activation)
	default:
		return decimalGTE(price, activation)
	}
}

func shouldUpdateAnchor(side string, price, anchor float64) bool {
	if price <= 0 || anchor <= 0 {
		return false
	}
	switch side {
	case "short":
		return decimalLT(price, anchor)
	default:
		return decimalGT(price, anchor)
	}
}

func trailingStopFor(side string, anchor, pct float64) float64 {
	if anchor <= 0 || pct <= 0 {
		return 0
	}
	base := decFromFloat(anchor)
	pctDec := decFromFloat(pct)
	var factor decimal.Decimal
	switch side {
	case "short":
		factor = decOne.Add(pctDec)
	default:
		factor = decOne.Sub(pctDec)
	}
	return decToFloat(base.Mul(factor))
}

func shouldUpdateStop(side string, candidate, current float64) bool {
	if candidate <= 0 {
		return false
	}
	if current <= 0 {
		return true
	}
	cand := decFromFloat(candidate)
	curr := decFromFloat(current)
	switch side {
	case "short":
		return cand.Cmp(curr.Sub(decimalEps)) < 0
	default:
		return cand.Cmp(curr.Add(decimalEps)) > 0
	}
}

func priceBreachedStop(side string, price, stop float64) bool {
	if stop <= 0 || price <= 0 {
		return false
	}
	switch side {
	case "short":
		return decimalGTE(price, stop)
	default:
		return decimalLTE(price, stop)
	}
}
