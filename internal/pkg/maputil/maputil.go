// Package maputil provides type-safe helpers for reading values from map[string]any.
package maputil

import (
	"fmt"
	"strconv"
	"strings"
)

// String extracts a string value from params, returning empty string if not found.
func String(params map[string]any, key string) string {
	if params == nil {
		return ""
	}
	raw, ok := params[key]
	if !ok {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", raw))
}

// Int extracts an integer value from params, returning 0 if not found or invalid.
func Int(params map[string]any, key string) int {
	if params == nil {
		return 0
	}
	raw, ok := params[key]
	if !ok {
		return 0
	}
	switch v := raw.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(v))
		return n
	default:
		n, _ := strconv.Atoi(strings.TrimSpace(fmt.Sprintf("%v", v)))
		return n
	}
}

// Float extracts a float64 value from params, returning 0 if not found or invalid.
func Float(params map[string]any, key string) float64 {
	if params == nil {
		return 0
	}
	raw, ok := params[key]
	if !ok {
		return 0
	}
	switch v := raw.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return f
	default:
		f, _ := strconv.ParseFloat(strings.TrimSpace(fmt.Sprintf("%v", v)), 64)
		return f
	}
}

// StringSlice extracts a slice of strings from params.
func StringSlice(params map[string]any, key string) []string {
	if params == nil {
		return nil
	}
	raw, ok := params[key]
	if !ok {
		return nil
	}
	switch val := raw.(type) {
	case []string:
		out := make([]string, 0, len(val))
		for _, item := range val {
			if s := strings.TrimSpace(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(val))
		for _, item := range val {
			str := strings.TrimSpace(fmt.Sprintf("%v", item))
			if str != "" {
				out = append(out, str)
			}
		}
		return out
	default:
		parts := strings.Split(fmt.Sprintf("%v", val), ",")
		out := make([]string, 0, len(parts))
		for _, item := range parts {
			if s := strings.TrimSpace(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
}
