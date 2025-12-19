package handlers

import (
	"fmt"
	"strings"
)

func validateModeParam(params map[string]any) error {
	if params == nil {
		return fmt.Errorf("缺少参数")
	}
	raw, ok := params["mode"]
	if !ok {
		return nil
	}
	mode, ok := raw.(string)
	if !ok {
		return fmt.Errorf("mode 必须为字符串")
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != "" && mode != "take_profit" && mode != "stop_loss" {
		return fmt.Errorf("mode 只能是 take_profit 或 stop_loss")
	}
	return nil
}

func validateTrailingPctParams(trigger, trail float64) error {
	if trigger <= 0 {
		return fmt.Errorf("trigger_pct 需 >0")
	}
	if trail <= 0 {
		return fmt.Errorf("trail_pct 需 >0")
	}
	if trail >= trigger {
		return fmt.Errorf("trail_pct 需小于 trigger_pct")
	}
	if trigger > 0.25 {
		return fmt.Errorf("trigger_pct 不能超过 25%%，当前=%.2f%%", trigger*100)
	}
	if trigger < 0.005 {
		return fmt.Errorf("trigger_pct 至少 0.5%%，当前=%.2f%%", trigger*100)
	}
	if trail < 0.002 {
		return fmt.Errorf("trail_pct 至少 0.2%%，当前=%.2f%%", trail*100)
	}
	return nil
}

func validateTrailingATRParams(params map[string]any) error {
	atrVal, okATR := number(params["atr_value"])
	triggerMul, okTrigMul := number(params["trigger_multiplier"])
	trailMul, okTrailMul := number(params["trail_multiplier"])
	if !okATR || !okTrigMul || !okTrailMul || atrVal <= 0 || triggerMul <= 0 || trailMul <= 0 {
		return fmt.Errorf("需提供 trigger_pct/trail_pct 或 atr_value+multiplier")
	}
	if triggerMul < minATRTriggerMultiplier || triggerMul > maxATRTriggerMultiplier {
		return fmt.Errorf("trigger_multiplier 需位于 [%.1f, %.1f]", minATRTriggerMultiplier, maxATRTriggerMultiplier)
	}
	if trailMul < minATRTrailMultiplier {
		return fmt.Errorf("trail_multiplier 需 >= %.1f", minATRTrailMultiplier)
	}
	if trailMul >= triggerMul {
		return fmt.Errorf("trail_multiplier 需小于 trigger_multiplier")
	}
	return nil
}
