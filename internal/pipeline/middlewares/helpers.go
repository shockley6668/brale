package middlewares

import (
	"fmt"
	"strings"

	"brale/internal/market"
)

func closes(candles []market.Candle) []float64 {
	out := make([]float64, len(candles))
	for i, c := range candles {
		out[i] = c.Close
	}
	return out
}

func safeSymbol(symbol string) string {
	return strings.ToUpper(strings.TrimSpace(symbol))
}

func formatFeature(symbol, text string) string {
	s := safeSymbol(symbol)
	if s == "" {
		return text
	}
	return fmt.Sprintf("[%s] %s", s, strings.TrimSpace(text))
}
