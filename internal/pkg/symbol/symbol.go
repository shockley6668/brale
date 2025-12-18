// Package symbol provides unified symbol format conversion for different exchanges.
//
// The package defines three main formats:
//   - Internal: ETH/USDT (standard format used in configuration and business logic)
//   - Binance:  ETHUSDT  (format required by Binance API)
//   - Freqtrade: ETH/USDT:USDT (format for futures contracts in Freqtrade)
package symbol

import (
	"strings"
)

// Format represents the symbol format type.
type Format string

const (
	FormatInternal  Format = "internal"  // ETH/USDT
	FormatBinance   Format = "binance"   // ETHUSDT
	FormatFreqtrade Format = "freqtrade" // ETH/USDT:USDT
)

// Converter defines the interface for symbol format conversion.
type Converter interface {
	// ToExchange converts internal format to exchange-specific format.
	ToExchange(internal string) string
	// FromExchange converts exchange-specific format to internal format.
	FromExchange(raw string) string
	// Format returns the format type this converter handles.
	Format() Format
}

// Symbol represents a parsed trading pair.
type Symbol struct {
	Base  string // Base currency (e.g., ETH)
	Quote string // Quote currency (e.g., USDT)
}

// Internal returns the internal format string (e.g., "ETH/USDT").
func (s Symbol) Internal() string {
	if s.Base == "" || s.Quote == "" {
		return ""
	}
	return s.Base + "/" + s.Quote
}

// Binance returns the Binance format string (e.g., "ETHUSDT").
func (s Symbol) Binance() string {
	if s.Base == "" || s.Quote == "" {
		return ""
	}
	return s.Base + s.Quote
}

// Parse parses any format string into a Symbol struct.
// Supports: ETH/USDT, ETHUSDT, ETH/USDT:USDT, eth/usdt
func Parse(s string) Symbol {
	s = strings.ToUpper(strings.TrimSpace(s))
	if s == "" {
		return Symbol{}
	}

	// Handle Freqtrade format: ETH/USDT:USDT
	if idx := strings.Index(s, ":"); idx >= 0 {
		s = s[:idx]
	}

	// Handle internal format: ETH/USDT
	if parts := strings.SplitN(s, "/", 2); len(parts) == 2 {
		return Symbol{
			Base:  strings.TrimSpace(parts[0]),
			Quote: strings.TrimSpace(parts[1]),
		}
	}

	// Handle Binance format: ETHUSDT
	// Try common quote currencies in order of specificity
	quoteCurrencies := []string{"USDT", "BUSD", "USDC", "TUSD", "BTC", "ETH", "BNB"}
	for _, quote := range quoteCurrencies {
		if strings.HasSuffix(s, quote) && len(s) > len(quote) {
			return Symbol{
				Base:  s[:len(s)-len(quote)],
				Quote: quote,
			}
		}
	}

	// Unable to parse
	return Symbol{}
}

// Normalize converts any format to internal format (ETH/USDT).
// Returns empty string if parsing fails.
func Normalize(s string) string {
	return Parse(s).Internal()
}

// NormalizeList normalizes a list of symbols, removes duplicates and empty entries.
func NormalizeList(symbols []string) []string {
	if len(symbols) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(symbols))
	out := make([]string, 0, len(symbols))
	for _, s := range symbols {
		norm := Normalize(s)
		if norm == "" {
			// Fallback: just uppercase if we can't parse
			norm = strings.ToUpper(strings.TrimSpace(s))
			if norm == "" {
				continue
			}
		}
		if _, ok := seen[norm]; ok {
			continue
		}
		seen[norm] = struct{}{}
		out = append(out, norm)
	}
	return out
}

// IsValid returns true if the string can be parsed as a valid symbol.
func IsValid(s string) bool {
	sym := Parse(s)
	return sym.Base != "" && sym.Quote != ""
}
