package symbol

import "strings"

type GateConverter struct{}

func (GateConverter) ToExchange(internal string) string {
	s := strings.ToUpper(strings.TrimSpace(internal))
	if s == "" {
		return ""
	}
	return strings.ReplaceAll(s, "/", "_")
}

func (GateConverter) FromExchange(raw string) string {
	s := strings.ToUpper(strings.TrimSpace(raw))
	if s == "" {
		return ""
	}
	if strings.Contains(s, "/") {
		return s
	}
	if strings.Contains(s, "_") {
		parts := strings.SplitN(s, "_", 2)
		if len(parts) == 2 {
			return parts[0] + "/" + parts[1]
		}
	}
	return Parse(s).Internal()
}

func (GateConverter) Format() Format {
	return FormatGate
}

var Gate = GateConverter{}
