package symbol

import "strings"

type BinanceConverter struct{}

func (BinanceConverter) ToExchange(internal string) string {
	s := strings.ToUpper(strings.TrimSpace(internal))
	return strings.ReplaceAll(s, "/", "")
}

func (BinanceConverter) FromExchange(raw string) string {
	return Parse(raw).Internal()
}

func (BinanceConverter) Format() Format {
	return FormatBinance
}

var Binance = BinanceConverter{}
