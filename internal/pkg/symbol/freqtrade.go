package symbol

import (
	"fmt"
	"strings"
)

const DefaultStakeCurrency = "USDT"

type FreqtradeConverter struct {
	StakeCurrency string
}

func NewFreqtradeConverter(stakeCurrency string) FreqtradeConverter {
	return FreqtradeConverter{
		StakeCurrency: strings.ToUpper(strings.TrimSpace(stakeCurrency)),
	}
}

func (c FreqtradeConverter) ToExchange(internal string) string {
	s := strings.ToUpper(strings.TrimSpace(internal))
	if s == "" {
		return ""
	}

	stake := strings.TrimSpace(c.StakeCurrency)
	if stake == "" {
		stake = DefaultStakeCurrency
	}

	if strings.Contains(s, ":") {
		return s
	}

	sym := Parse(s)
	if sym.Quote == "" {
		return s
	}

	if sym.Quote == stake {
		return fmt.Sprintf("%s:%s", s, stake)
	}

	return s
}

func (c FreqtradeConverter) FromExchange(raw string) string {
	s := strings.ToUpper(strings.TrimSpace(raw))
	if s == "" {
		return ""
	}

	if idx := strings.Index(s, ":"); idx >= 0 {
		s = s[:idx]
	}

	if !strings.Contains(s, "/") {

		sym := Parse(s)
		if sym.Base != "" && sym.Quote != "" {
			return sym.Internal()
		}
	}

	return s
}

func (c FreqtradeConverter) Format() Format {
	return FormatFreqtrade
}

func Freqtrade(stakeCurrency string) FreqtradeConverter {
	return NewFreqtradeConverter(stakeCurrency)
}
