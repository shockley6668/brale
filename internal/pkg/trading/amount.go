package trading

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
