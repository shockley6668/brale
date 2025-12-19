package symbol

import (
	"strings"
)

type Format string

const (
	FormatInternal  Format = "internal"
	FormatBinance   Format = "binance"
	FormatFreqtrade Format = "freqtrade"
)

type Converter interface {
	ToExchange(internal string) string

	FromExchange(raw string) string

	Format() Format
}

type Symbol struct {
	Base  string
	Quote string
}

func (s Symbol) Internal() string {
	if s.Base == "" || s.Quote == "" {
		return ""
	}
	return s.Base + "/" + s.Quote
}

func (s Symbol) Binance() string {
	if s.Base == "" || s.Quote == "" {
		return ""
	}
	return s.Base + s.Quote
}

func Parse(s string) Symbol {
	s = strings.ToUpper(strings.TrimSpace(s))
	if s == "" {
		return Symbol{}
	}

	if idx := strings.Index(s, ":"); idx >= 0 {
		s = s[:idx]
	}

	if parts := strings.SplitN(s, "/", 2); len(parts) == 2 {
		return Symbol{
			Base:  strings.TrimSpace(parts[0]),
			Quote: strings.TrimSpace(parts[1]),
		}
	}

	quoteCurrencies := []string{"USDT", "BUSD", "USDC", "TUSD", "BTC", "ETH", "BNB"}
	for _, quote := range quoteCurrencies {
		if strings.HasSuffix(s, quote) && len(s) > len(quote) {
			return Symbol{
				Base:  s[:len(s)-len(quote)],
				Quote: quote,
			}
		}
	}

	return Symbol{}
}

func Normalize(s string) string {
	return Parse(s).Internal()
}

func NormalizeList(symbols []string) []string {
	if len(symbols) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(symbols))
	out := make([]string, 0, len(symbols))
	for _, s := range symbols {
		norm := Normalize(s)
		if norm == "" {

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

func IsValid(s string) bool {
	sym := Parse(s)
	return sym.Base != "" && sym.Quote != ""
}
