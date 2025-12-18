// Package trading provides trading calculation utilities.
package trading

// CalcCloseAmount computes the close amount based on ratio and position data.
// If isInitialRatio is true, the calculation uses initialAmount as the base.
// The result is capped at the current position amount.
func CalcCloseAmount(currentAmount, initialAmount, ratio float64, isInitialRatio bool) float64 {
	if currentAmount <= 0 || ratio <= 0 {
		return 0
	}

	base := currentAmount
	if isInitialRatio && initialAmount > 0 {
		base = initialAmount
	}

	amount := base * ratio
	if amount > currentAmount {
		amount = currentAmount
	}
	return amount
}
