package decision

import (
	"encoding/json"
	"strconv"
	"strings"
)

func coerceString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		if x {
			return "true"
		}
		return "false"
	case map[string]any:
		return ""
	case []any:
		return ""
	default:
		return ""
	}
}

func coerceFloat64(v any) float64 {
	switch x := v.(type) {
	case nil:
		return 0
	case float64:
		return x
	case float32:
		return float64(x)
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case json.Number:
		if f, err := x.Float64(); err == nil {
			return f
		}
		return 0
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return 0
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return f
		}
		return 0
	default:
		return 0
	}
}

func coerceInt(v any) int {
	return int(coerceFloat64(v))
}

func (d *Decision) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	d.Symbol = coerceString(raw["symbol"])
	d.Action = coerceString(raw["action"])
	d.ContextTag = coerceString(raw["context_tag"])
	d.Profile = coerceString(raw["profile"])
	d.Leverage = coerceInt(raw["leverage"])
	d.PositionSizeUSD = coerceFloat64(raw["position_size_usd"])
	d.CloseRatio = coerceFloat64(raw["close_ratio"])
	d.StopLoss = coerceFloat64(raw["stop_loss"])
	d.TakeProfit = coerceFloat64(raw["take_profit"])
	d.Confidence = coerceInt(raw["confidence"])
	d.Reasoning = coerceString(raw["reasoning"])

	if v, ok := raw["exit_plan"]; ok && v != nil {
		if b, err := json.Marshal(v); err == nil {
			var plan ExitPlanSpec
			if err := json.Unmarshal(b, &plan); err == nil && strings.TrimSpace(plan.ID) != "" {
				d.ExitPlan = &plan
			}
		}
	}
	return nil
}
