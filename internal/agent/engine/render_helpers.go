package engine

import (
	"brale/internal/pkg/utils"
	"fmt"
	"strings"
)

func summarizePlanParams(params map[string]any, entryPrice float64, side string) []string {
	if len(params) == 0 {
		return nil
	}
	lines := make([]string, 0, 4)
	if v, ok := utils.AsFloat(params["stop_loss_pct"]); ok && v != 0 {
		lines = append(lines, "· 初始止损 "+utils.FormatPercent(v)+approxPrice(entryPrice, v, side))
	}
	if v, ok := utils.AsFloat(params["final_stop_loss_pct"]); ok && v != 0 {
		lines = append(lines, "· 最终止损 "+utils.FormatPercent(v)+approxPrice(entryPrice, v, side))
	}
	if v, ok := utils.AsFloat(params["final_take_profit_pct"]); ok && v != 0 {
		lines = append(lines, "· 最终止盈 "+utils.FormatPercent(v)+approxPrice(entryPrice, v, side))
	}
	if v, ok := utils.AsFloat(params["take_profit_pct"]); ok && v != 0 {
		lines = append(lines, "· 止盈 "+utils.FormatPercent(v)+approxPrice(entryPrice, v, side))
	}
	if tiers := summarizeTiers(params["tiers"], entryPrice, side); tiers != "" {
		lines = append(lines, "· 分段止盈："+tiers)
	}
	if children := summarizePlanChildren(params["children"], entryPrice, side); len(children) > 0 {
		lines = append(lines, children...)
	}
	return lines
}

func summarizeTiers(raw any, entryPrice float64, side string) string {
	list, ok := raw.([]any)
	if !ok || len(list) == 0 {
		return ""
	}
	details := make([]string, 0, len(list))
	for idx, entry := range list {
		m, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		target, _ := utils.AsFloat(m["target"])
		ratio, _ := utils.AsFloat(m["ratio"])
		priceSuffix := approxPrice(entryPrice, target, side)
		details = append(details, fmt.Sprintf("T%d 目标%s%s · 比例%s", idx+1, utils.FormatPercent(target), priceSuffix, utils.FormatPercent(ratio)))
	}
	return strings.Join(details, "；")
}

func approxPrice(entry float64, pct float64, side string) string {
	price := targetPrice(entry, pct, side)
	if price <= 0 {
		return ""
	}
	return fmt.Sprintf(" ≈ %.2f", price)
}

func targetPrice(entry float64, pct float64, side string) float64 {
	if entry <= 0 {
		return 0
	}
	side = strings.ToLower(strings.TrimSpace(side))
	adj := pct
	if side == "short" {
		adj = -pct
	}
	return entry * (1 + adj)
}

func summarizePlanChildren(raw any, entryPrice float64, side string) []string {
	children, ok := raw.([]any)
	if !ok || len(children) == 0 {
		return nil
	}
	lines := make([]string, 0, len(children))
	for _, item := range children {
		child, ok := item.(map[string]any)
		if !ok {
			continue
		}
		component := strings.TrimSpace(fmt.Sprint(child["component"]))
		params, _ := child["params"].(map[string]any)
		lines = append(lines, summarizePlanComponent(component, params, entryPrice, side)...)
	}
	return lines
}

func summarizePlanComponent(component string, params map[string]any, entryPrice float64, side string) []string {
	component = strings.TrimSpace(component)
	if component == "" {
		if nested := summarizePlanChildren(params["children"], entryPrice, side); len(nested) > 0 {
			return nested
		}
		return nil
	}
	switch component {
	case "tp_tiers", "tp_single", "sl_tiers", "sl_single":
		return summarizeTierComponent(component, params, entryPrice, side)
	case "tp_atr", "sl_atr":
		return summarizeATRComponent(component, params)
	default:
		if nested := summarizePlanChildren(params["children"], entryPrice, side); len(nested) > 0 {
			return nested
		}
		if tiers := summarizeTierComponent(component, params, entryPrice, side); len(tiers) > 0 {
			return tiers
		}
		return nil
	}
}

type tierEntry struct {
	Price float64
	Ratio float64
}

func summarizeTierComponent(component string, params map[string]any, entryPrice float64, side string) []string {
	entries := parseTierEntries(params["tiers"], entryPrice, side)
	if len(entries) == 0 {
		return nil
	}
	label := tierComponentPrefix(component)
	lines := make([]string, 0, len(entries))
	for idx, entry := range entries {
		stage := label
		if len(entries) > 1 {
			stage = fmt.Sprintf("%s#%d", label, idx+1)
		}
		line := fmt.Sprintf("· %s @%.4f", stage, entry.Price)
		if entry.Ratio > 0 {
			line += fmt.Sprintf(" · 比例%s", utils.FormatRatio(entry.Ratio))
		}
		lines = append(lines, line)
	}
	return lines
}

func parseTierEntries(raw any, entryPrice float64, side string) []tierEntry {
	list, ok := raw.([]any)
	if !ok || len(list) == 0 {
		return nil
	}
	out := make([]tierEntry, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		price, _ := utils.NumberFromKeys(m, "target_price", "targetPrice", "price")
		if price <= 0 && entryPrice > 0 {
			if pct, ok := utils.NumberFromKeys(m, "target", "target_pct"); ok {
				price = targetPrice(entryPrice, pct, side)
			}
		}
		if price <= 0 {
			continue
		}
		ratio, _ := utils.AsFloat(m["ratio"])
		out = append(out, tierEntry{Price: price, Ratio: ratio})
	}
	return out
}

func summarizeATRComponent(component string, params map[string]any) []string {
	if len(params) == 0 {
		return nil
	}
	label := tierComponentPrefix(component)
	atrValue, _ := utils.AsFloat(params["atr_value"])
	trigger, _ := utils.AsFloat(params["trigger_multiplier"])
	trail, _ := utils.AsFloat(params["trail_multiplier"])
	line := fmt.Sprintf("· %s ATR=%.4f", label, atrValue)
	if trigger > 0 {
		line += fmt.Sprintf(" · 触发%.2fx", trigger)
	}
	if trail > 0 {
		line += fmt.Sprintf(" · 追踪%.2fx", trail)
	}
	return []string{line}
}

func tierComponentPrefix(component string) string {
	switch component {
	case "tp_tiers", "tp_single":
		return "止盈"
	case "sl_tiers", "sl_single":
		return "止损"
	case "tp_atr":
		return "ATR 止盈"
	case "sl_atr":
		return "ATR 止损"
	default:
		return strings.ToUpper(component)
	}
}
