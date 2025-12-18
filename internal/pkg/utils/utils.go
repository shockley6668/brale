package utils

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func AsFloat(val interface{}) (float64, bool) {
	switch v := val.(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint64:
		return float64(v), true
	case float32:
		return float64(v), true
	case json.Number:
		if f, err := v.Float64(); err == nil {
			return f, true
		}
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return f, true
		}
	}
	return 0, false
}

func AsString(val interface{}) string {
	switch v := val.(type) {
	case string:
		return v
	case float64:
		return fmt.Sprintf("%f", v)
	case int:
		return fmt.Sprintf("%d", v)
	default:
		if val == nil {
			return ""
		}
		return fmt.Sprintf("%v", v)
	}
}

func FormatFloat(val float64) string {
	if val == 0 {
		return "0"
	}
	return fmt.Sprintf("%.4f", val)
}

func FormatPercent(val float64) string {
	return fmt.Sprintf("%+.2f%%", val*100)
}

func FormatRatio(val float64) string {
	return fmt.Sprintf("%.0f%%", val*100)
}

func NumberFromKeys(m map[string]any, keys ...string) (float64, bool) {
	if len(m) == 0 {
		return 0, false
	}
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if f, ok := AsFloat(v); ok {
				return f, true
			}
		}
	}
	return 0, false
}
