package trading

import "math"

// CalcCloseAmount computes close size using ratios over the initial position.
// base = initialAmount when isInitialRatio=true (fallback to currentAmount),
// then clamps to currentAmount to avoid over-closing.
func CalcCloseAmount(currentAmount, initialAmount, ratio float64, isInitialRatio bool) float64 {
	if ratio <= 0 || currentAmount <= 0 {
		return 0
	}
	if ratio > 1 {
		ratio = 1
	}

	base := currentAmount
	if isInitialRatio && initialAmount > 0 {
		base = initialAmount
	}

	target := base * ratio
	return math.Min(target, currentAmount)
}
