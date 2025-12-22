package config

import "strings"

// NormalizePersonaRole normalizes role aliases to canonical values.
// Allowed: indicator, mechanics, structure_pattern.
func NormalizePersonaRole(role string) string {
	r := strings.ToLower(strings.TrimSpace(role))
	switch r {
	case "indicator", "indicators", "momentum":
		return "indicator"
	case "mechanics", "derivatives", "funding", "oi":
		return "mechanics"
	case "structure_pattern", "structure-pattern", "structure", "pattern", "trend", "trend_pattern", "trend-pattern":
		return "structure_pattern"
	default:
		return ""
	}
}

// NormalizePersonaStage normalizes stage name to canonical value.
// Allowed: indicator, pattern, trend, mechanics.
func NormalizePersonaStage(stage string) string {
	s := strings.ToLower(strings.TrimSpace(stage))
	switch s {
	case "indicator", "pattern", "trend", "mechanics":
		return s
	default:
		return ""
	}
}
