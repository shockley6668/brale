package symbol

import "strings"

// BinanceConverter converts between internal format and Binance format.
// Internal: ETH/USDT -> Binance: ETHUSDT
type BinanceConverter struct{}

// ToExchange converts internal format (ETH/USDT) to Binance format (ETHUSDT).
func (BinanceConverter) ToExchange(internal string) string {
	s := strings.ToUpper(strings.TrimSpace(internal))
	return strings.ReplaceAll(s, "/", "")
}

// FromExchange converts Binance format (ETHUSDT) to internal format (ETH/USDT).
// Uses Parse to intelligently detect the quote currency.
func (BinanceConverter) FromExchange(raw string) string {
	return Parse(raw).Internal()
}

// Format returns FormatBinance.
func (BinanceConverter) Format() Format {
	return FormatBinance
}

// Binance is a default BinanceConverter instance for convenience.
var Binance = BinanceConverter{}
