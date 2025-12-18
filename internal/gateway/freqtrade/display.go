package freqtrade

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"brale/internal/strategy/exit"
)

// decodePlanReason parses plan:component:event format and returns display strings.
func decodePlanReason(reason string) (string, string) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "", ""
	}
	reason = strings.TrimPrefix(reason, "plan:")
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "", ""
	}
	parts := strings.Split(reason, ":")
	component := ""
	event := ""
	if len(parts) > 0 {
		component = parts[0]
	}
	if len(parts) > 1 {
		event = parts[1]
	}
	return renderStageFromComponent(component, event)
}

func componentDisplay(component string) string {
	component = strings.TrimSpace(component)
	if component == "" {
		return "计划事件"
	}
	base := component
	stage := ""
	if idx := strings.Index(component, "."); idx >= 0 {
		base = component[:idx]
		stage = component[idx+1:]
	}
	prefix := planComponentLabel(base)
	if stage == "" {
		return prefix
	}
	return fmt.Sprintf("%s · %s", prefix, humanizeTier(stage))
}

func renderStageFromComponent(component, event string) (string, string) {
	component = strings.TrimSpace(component)
	event = strings.TrimSpace(event)
	detail := componentDisplay(component)
	if detail == "" {
		detail = "计划事件"
	}
	if event == "" {
		return detail, detail
	}
	eventLabel := eventDisplay(event)
	return fmt.Sprintf("%s %s", detail, eventLabel), fmt.Sprintf("%s · %s", detail, eventLabel)
}

func planComponentLabel(component string) string {
	switch strings.ToLower(strings.TrimSpace(component)) {
	case "tp_tiers":
		return "分段止盈"
	case "tp_single":
		return "单段止盈"
	case "sl_tiers":
		return "分段止损"
	case "sl_single":
		return "固定止损"
	case "tp_atr":
		return "ATR 止盈"
	case "sl_atr":
		return "ATR 止损"
	default:
		return strings.ToUpper(strings.TrimSpace(component))
	}
}

func eventDisplay(event string) string {
	switch event {
	case exit.PlanEventTypeTierHit:
		return "触发"
	case exit.PlanEventTypeTakeProfit:
		return "止盈"
	case exit.PlanEventTypeStopLoss:
		return "止损"
	case exit.PlanEventTypeFinalTakeProfit:
		return "最终止盈"
	case exit.PlanEventTypeFinalStopLoss:
		return "最终止损"
	default:
		return event
	}
}

func humanizeTier(alias string) string {
	alias = strings.TrimSpace(strings.ToLower(alias))
	if strings.HasPrefix(alias, "tier") {
		if idx, err := strconv.Atoi(strings.TrimPrefix(alias, "tier")); err == nil {
			switch idx {
			case 1:
				return "第一目标"
			case 2:
				return "第二目标"
			case 3:
				return "第三目标"
			default:
				return fmt.Sprintf("第%d目标", idx)
			}
		}
	}
	return strings.ToUpper(alias)
}

func formatSignedValue(v float64) string {
	if math.Abs(v) < 1e-9 {
		return "0.00 USDT"
	}
	sign := ""
	if v > 0 {
		sign = "+"
	}
	return fmt.Sprintf("%s%.2f USDT", sign, v)
}

func formatSignedPercent(v float64) string {
	if math.Abs(v) < 1e-9 {
		return "0.00%"
	}
	sign := ""
	if v > 0 {
		sign = "+"
	}
	return fmt.Sprintf("%s%.2f%%", sign, v)
}

func clampAmount(v float64) float64 {
	if v <= 0 {
		return 0
	}
	return v
}
