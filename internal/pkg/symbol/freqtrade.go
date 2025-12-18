package symbol

import (
	"fmt"
	"strings"
)

// DefaultStakeCurrency is the default stake currency used when none is configured.
const DefaultStakeCurrency = "USDT"

// FreqtradeConverter converts between internal format and Freqtrade format.
// Internal: ETH/USDT -> Freqtrade: ETH/USDT:USDT
type FreqtradeConverter struct {
	StakeCurrency string // e.g., "USDT" (defaults to DefaultStakeCurrency if empty)
}

// NewFreqtradeConverter creates a new FreqtradeConverter with the specified stake currency.
func NewFreqtradeConverter(stakeCurrency string) FreqtradeConverter {
	return FreqtradeConverter{
		StakeCurrency: strings.ToUpper(strings.TrimSpace(stakeCurrency)),
	}
}

// ToExchange converts internal format (ETH/USDT) to Freqtrade format (ETH/USDT:USDT).
func (c FreqtradeConverter) ToExchange(internal string) string {
	s := strings.ToUpper(strings.TrimSpace(internal))
	if s == "" {
		return ""
	}

	// Use configured stake currency, or default to USDT
	stake := strings.TrimSpace(c.StakeCurrency)
	if stake == "" {
		stake = DefaultStakeCurrency
	}

	// Already in Freqtrade format
	if strings.Contains(s, ":") {
		return s
	}

	// Parse and validate
	sym := Parse(s)
	if sym.Quote == "" {
		return s
	}

	// Only append stake currency if quote matches
	if sym.Quote == stake {
		return fmt.Sprintf("%s:%s", s, stake)
	}

	return s
}

// FromExchange converts Freqtrade format (ETH/USDT:USDT) to internal format (ETH/USDT).
func (c FreqtradeConverter) FromExchange(raw string) string {
	s := strings.ToUpper(strings.TrimSpace(raw))
	if s == "" {
		return ""
	}

	// Remove the :STAKE suffix
	if idx := strings.Index(s, ":"); idx >= 0 {
		s = s[:idx]
	}

	// Ensure it's in BASE/QUOTE format
	if !strings.Contains(s, "/") {
		// Try to parse and reconstruct
		sym := Parse(s)
		if sym.Base != "" && sym.Quote != "" {
			return sym.Internal()
		}
	}

	return s
}

// Format returns FormatFreqtrade.
func (c FreqtradeConverter) Format() Format {
	return FormatFreqtrade
}

// Freqtrade creates a FreqtradeConverter with the given stake currency.
// This is a convenience function for one-off conversions.
func Freqtrade(stakeCurrency string) FreqtradeConverter {
	return NewFreqtradeConverter(stakeCurrency)
}
