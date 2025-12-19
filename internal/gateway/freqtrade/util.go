package freqtrade

import (
	"strings"
	"time"

	symbolpkg "brale/internal/pkg/symbol"
)

func freqtradePairToSymbol(pair string) string {
	return symbolpkg.Normalize(pair)
}

func parseFreqtradeTime(raw string) time.Time {
	layouts := []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05"}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, strings.TrimSpace(raw)); err == nil {
			return t
		}
	}
	return time.Time{}
}

func firstNonZero(vals ...float64) float64 {
	for _, v := range vals {
		if v != 0 {
			return v
		}
	}
	return 0
}
