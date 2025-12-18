package freqtrade

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"brale/internal/decision"
)

func (m *Manager) effectiveEntryPrice(side string, marketPrice float64) float64 {
	price := marketPrice
	if price <= 0 {
		return 0
	}
	slip := m.cfg.EntrySlipPct
	if slip <= 0 {
		return price
	}
	switch strings.ToLower(strings.TrimSpace(side)) {
	case "short":
		return price * (1 - slip)
	default:
		return price * (1 + slip)
	}
}

func (m *Manager) validateInitialStopDistance(decision decision.Decision, side string, entryPrice float64) error {
	minPct := m.cfg.MinStopDistancePct
	if minPct <= 0 {
		return nil
	}
	if entryPrice <= 0 {
		return fmt.Errorf("entry_price 无效，无法校验止损距离")
	}
	side = strings.ToLower(strings.TrimSpace(side))
	if side != "long" && side != "short" {
		return fmt.Errorf("side 无效，无法校验止损距离: %s", side)
	}
	if decision.ExitPlan == nil || strings.TrimSpace(decision.ExitPlan.ID) == "" {
		return fmt.Errorf("缺少 exit_plan，无法校验止损距离")
	}
	distPct, err := initialStopDistancePct(decision.ExitPlan.Params, side, entryPrice)
	if err != nil {
		return err
	}
	if distPct < minPct {
		return fmt.Errorf("止损距离过小: %.4f%% < %.4f%%", distPct*100, minPct*100)
	}
	return nil
}

func initialStopDistancePct(planParams map[string]any, side string, entryPrice float64) (float64, error) {
	if len(planParams) == 0 {
		return 0, fmt.Errorf("exit_plan.params 为空，无法解析止损")
	}
	rawChildren, ok := planParams["children"]
	if !ok {
		return 0, fmt.Errorf("exit_plan.params.children 缺失，无法解析止损")
	}
	children, ok := rawChildren.([]any)
	if !ok || len(children) == 0 {
		return 0, fmt.Errorf("exit_plan.params.children 格式错误或为空，无法解析止损")
	}

	minDist := math.MaxFloat64
	foundStop := false
	for _, raw := range children {
		child, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		component := strings.ToLower(strings.TrimSpace(fmt.Sprint(child["component"])))
		params, _ := child["params"].(map[string]any)
		switch component {
		case "sl_single", "sl_tiers":
			dist, err := tierStopDistancePct(params, side, entryPrice)
			if err != nil {
				return 0, err
			}
			foundStop = true
			if dist < minDist {
				minDist = dist
			}
		case "sl_atr":
			dist, err := atrInitialStopDistancePct(params, entryPrice)
			if err != nil {
				return 0, err
			}
			foundStop = true
			if dist < minDist {
				minDist = dist
			}
		}
	}
	if !foundStop || minDist == math.MaxFloat64 {
		return 0, fmt.Errorf("exit_plan 缺少有效的止损组件（sl_*）")
	}
	return minDist, nil
}

func tierStopDistancePct(params map[string]any, side string, entryPrice float64) (float64, error) {
	if entryPrice <= 0 {
		return 0, fmt.Errorf("entry_price 无效")
	}
	rawTiers, ok := params["tiers"]
	if !ok {
		return 0, fmt.Errorf("止损 tiers 缺失")
	}
	tiers, ok := rawTiers.([]any)
	if !ok || len(tiers) == 0 {
		return 0, fmt.Errorf("止损 tiers 为空")
	}
	side = strings.ToLower(strings.TrimSpace(side))
	minDist := math.MaxFloat64
	for idx, raw := range tiers {
		tier, ok := raw.(map[string]any)
		if !ok {
			return 0, fmt.Errorf("止损 tier#%d 格式错误", idx+1)
		}
		target, ok := number(tier["target_price"])
		if !ok || target <= 0 {
			return 0, fmt.Errorf("止损 tier#%d target_price 无效", idx+1)
		}
		diff := target - entryPrice
		switch side {
		case "short":
			if diff <= 0 {
				return 0, fmt.Errorf("止损 tier#%d 目标价 %.4f 不符合 stop_loss 方向（short）", idx+1, target)
			}
		default:
			if diff >= 0 {
				return 0, fmt.Errorf("止损 tier#%d 目标价 %.4f 不符合 stop_loss 方向（long）", idx+1, target)
			}
		}
		dist := math.Abs(diff / entryPrice)
		if dist < minDist {
			minDist = dist
		}
	}
	if minDist == math.MaxFloat64 {
		return 0, fmt.Errorf("止损 tiers 无有效目标")
	}
	return minDist, nil
}

func atrInitialStopDistancePct(params map[string]any, entryPrice float64) (float64, error) {
	if entryPrice <= 0 {
		return 0, fmt.Errorf("entry_price 无效")
	}
	mode := strings.ToLower(strings.TrimSpace(fmt.Sprint(params["mode"])))
	if mode != "" && mode != "stop_loss" {
		return 0, fmt.Errorf("sl_atr mode 必须为 stop_loss 或省略")
	}
	atr, ok := number(params["atr_value"])
	if !ok || atr <= 0 {
		return 0, fmt.Errorf("sl_atr 缺少有效 atr_value")
	}
	initialMul, ok := number(params["initial_stop_multiplier"])
	if !ok || initialMul <= 0 {
		return 0, fmt.Errorf("sl_atr 缺少 initial_stop_multiplier（必须提供初始止损，否则拒绝开仓）")
	}
	return (atr * initialMul) / entryPrice, nil
}

func number(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case uint:
		return float64(x), true
	case uint64:
		return float64(x), true
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return 0, false
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}
