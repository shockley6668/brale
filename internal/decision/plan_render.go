package decision

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"brale/internal/logger"
	formatutil "brale/internal/pkg/format"
)

type planStateView struct {
	PlanID             string              `json:"plan_id"`
	Version            int                 `json:"version"`
	Components         []planComponentView `json:"components"`
	EditableComponents []string            `json:"editable_components"`
	Instruction        string              `json:"instruction"`
}

type planComponentView struct {
	Component string          `json:"component"`
	Status    string          `json:"status"`
	Params    json.RawMessage `json:"params"`
	State     json.RawMessage `json:"state"`
}

type tierComponentStateView struct {
	Name            string  `json:"name"`
	TargetPrice     float64 `json:"target_price"`
	Ratio           float64 `json:"ratio"`
	Status          string  `json:"status"`
	TriggeredAt     int64   `json:"triggered_at"`
	TriggerPrice    float64 `json:"trigger_price"`
	RemainingRatio  float64 `json:"remaining_ratio"`
	Mode            string  `json:"mode"`
	PendingOrderID  string  `json:"pending_order_id"`
	TrailingStop    float64 `json:"trailing_stop_price"`
	LastEvent       string  `json:"last_event"`
	ExecutedRatio   float64 `json:"executed_ratio"`
	EntryPrice      float64 `json:"entry_price"`
	Symbol          string  `json:"symbol"`
	Side            string  `json:"side"`
	TriggerAttempts int     `json:"trigger_attempts"`
}

type atrComponentStateView struct {
	Mode                    string  `json:"mode"`
	TrailingStopPrice       float64 `json:"trailing_stop_price"`
	TrailingActivationPrice float64 `json:"trailing_activation_price"`
	StopLossPrice           float64 `json:"stop_loss"`
	TakeProfitPrice         float64 `json:"take_profit"`
	TriggerPct              float64 `json:"trigger_pct"`
	TrailPct                float64 `json:"trail_pct"`
	TrailingActive          bool    `json:"trailing_active"`
}

func renderPlanStateSummary(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var plans []planStateView
	if err := json.Unmarshal([]byte(raw), &plans); err != nil {
		logger.Debugf("renderPlanStateSummary: parse failed: %v", err)
		return ""
	}
	var b strings.Builder
	for _, plan := range plans {
		if block := renderPlanStateBlock(plan); block != "" {
			if b.Len() > 0 {
				b.WriteString("    \n")
			}
			b.WriteString(block)
		}
	}
	return b.String()
}

func renderPlanStateBlock(plan planStateView) string {
	editable := make(map[string]bool, len(plan.EditableComponents))
	for _, name := range plan.EditableComponents {
		key := canonicalComponentName(name)
		if key == "" {
			continue
		}
		editable[key] = true
	}
	triggered := make([]string, 0, len(plan.Components))
	waiting := make([]string, 0, len(plan.Components))
	for _, comp := range plan.Components {
		base, stage := splitComponentName(comp.Component)
		if base == "" {
			continue
		}
		key := canonicalComponentName(comp.Component)
		switch base {
		case "tp_tiers", "tp_single", "sl_tiers", "sl_single":
			trig, edit := describeTierComponent(base, stage, comp, editable[key])
			if trig != "" {
				triggered = append(triggered, trig)
			}
			if edit != "" {
				waiting = append(waiting, edit)
			}
		case "tp_atr", "sl_atr":
			trig, edit := describeATRComponent(base, comp, editable[key])
			if trig != "" {
				triggered = append(triggered, trig)
			}
			if edit != "" {
				waiting = append(waiting, edit)
			}
		default:
			continue
		}
	}
	if len(triggered) == 0 && len(waiting) == 0 {
		return ""
	}
	var b strings.Builder
	title := strings.ToUpper(strings.TrimSpace(plan.PlanID))
	if title == "" {
		title = "PLAN"
	}
	b.WriteString(fmt.Sprintf("    策略：%s (v%d)\n", title, plan.Version))
	b.WriteString(renderPlanSection("已触发", triggered, "暂无"))
	b.WriteString(renderPlanSection("等待触发", waiting, "暂无待触发组件"))
	return b.String()
}

func renderPlanSection(title string, lines []string, emptyText string) string {
	var b strings.Builder
	b.WriteString("    " + title)
	if len(lines) == 0 {
		b.WriteString(": " + emptyText + "\n")
		return b.String()
	}
	b.WriteString(":\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		b.WriteString("      - " + line + "\n")
	}
	return b.String()
}

func describeTierComponent(base, stage string, comp planComponentView, editable bool) (string, string) {
	if stage == "" {
		return "", ""
	}
	state := tierComponentStateView{}
	if len(comp.State) > 0 {
		_ = json.Unmarshal(comp.State, &state)
	}
	target := state.TargetPrice
	if target <= 0 {
		target = state.TriggerPrice
	}
	ratio := state.Ratio
	if ratio <= 0 {
		ratio = state.RemainingRatio
	}
	label := buildTierLabel(base, stage)
	status := normalizeStatus(state.Status, comp.Status)
	if tierTriggered(status, state) {
		price := state.TriggerPrice
		if price <= 0 {
			price = target
		}
		ratioLabel := ratioPercent(ratio)
		line := fmt.Sprintf("%s 已触发 触达价 %.2f", label, price)
		if ratioLabel != "" {
			line += fmt.Sprintf(" 比例 %s", ratioLabel)
		}
		return line, ""
	}
	priceText := "--"
	if target > 0 {
		priceText = fmt.Sprintf("%.2f", target)
	}
	ratioLabel := ratioPercent(ratio)
	line := fmt.Sprintf("%s 待触发 目标价 %s", label, priceText)
	if ratioLabel != "" {
		line += fmt.Sprintf(" 比例 %s", ratioLabel)
	}
	return "", line
}

func describeATRComponent(base string, comp planComponentView, editable bool) (string, string) {
	label := componentPrefix(base)
	state := atrComponentStateView{}
	if len(comp.State) > 0 {
		_ = json.Unmarshal(comp.State, &state)
	}
	status := normalizeStatus(comp.Status)
	triggered := atrTriggeredLine(label, state, status)
	if !editable && triggered != "" {
		return triggered, ""
	}
	details := atrDetailItems(comp, state)
	line := atrDetailLine(label, details)
	line = appendATRStateLine(line, state)
	return triggered, line
}

func atrTriggeredLine(label string, state atrComponentStateView, status string) string {
	if !isTriggeredStatus(status) {
		return ""
	}
	switch {
	case state.TrailingStopPrice > 0:
		return fmt.Sprintf("%s 已触发，当前保护价 %.2f", label, state.TrailingStopPrice)
	case state.StopLossPrice > 0:
		return fmt.Sprintf("%s 已触发，当前止损 %.2f", label, state.StopLossPrice)
	case state.TakeProfitPrice > 0:
		return fmt.Sprintf("%s 已触发，当前止盈 %.2f", label, state.TakeProfitPrice)
	default:
		return fmt.Sprintf("%s 已触发", label)
	}
}

func atrDetailItems(comp planComponentView, state atrComponentStateView) []string {
	params := decodeRawMap(comp.Params)
	atrValue := fetchFloat(params, "atr_value")
	triggerMul := fetchFloat(params, "trigger_multiplier")
	trailMul := fetchFloat(params, "trail_multiplier")
	initialMul := fetchFloat(params, "initial_stop_multiplier")
	details := make([]string, 0, 4)
	if atrValue > 0 {
		details = append(details, fmt.Sprintf("ATR=%.2f", atrValue))
	}
	if triggerMul > 0 {
		details = append(details, fmt.Sprintf("触发%.2fx", triggerMul))
	} else if state.TriggerPct > 0 {
		details = append(details, fmt.Sprintf("触发%.2f%%", state.TriggerPct*100))
	}
	if trailMul > 0 {
		details = append(details, fmt.Sprintf("追踪%.2fx", trailMul))
	} else if state.TrailPct > 0 {
		details = append(details, fmt.Sprintf("追踪%.2f%%", state.TrailPct*100))
	}
	if initialMul > 0 {
		details = append(details, fmt.Sprintf("初始%.2fx", initialMul))
	}
	return details
}

func atrDetailLine(label string, details []string) string {
	if len(details) == 0 {
		return fmt.Sprintf("%s: 参数待定", label)
	}
	return fmt.Sprintf("%s: %s", label, strings.Join(details, " · "))
}

func appendATRStateLine(line string, state atrComponentStateView) string {
	if state.TrailingActive && state.TrailingStopPrice > 0 {
		return line + fmt.Sprintf(" · 已激活保护价 %.2f", state.TrailingStopPrice)
	}
	if state.TrailingActivationPrice > 0 {
		return line + fmt.Sprintf(" · 激活价 %.2f", state.TrailingActivationPrice)
	}
	return line
}

func splitComponentName(name string) (string, string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ""
	}
	base := name
	stage := ""
	if idx := strings.Index(name, "."); idx >= 0 {
		base = name[:idx]
		stage = name[idx+1:]
	}
	return strings.ToLower(base), stage
}

func componentPrefix(base string) string {
	switch base {
	case "tp_tiers", "tp_single":
		return "止盈"
	case "sl_tiers", "sl_single":
		return "止损"
	case "tp_atr":
		return "ATR 止盈"
	case "sl_atr":
		return "ATR 止损"
	default:
		return strings.ToUpper(base)
	}
}

func buildTierLabel(base, stage string) string {
	prefix := componentPrefix(base)
	if base == "tp_single" || base == "sl_single" {
		return prefix
	}
	suffix := humanizeStage(stage)
	if suffix == "" {
		return prefix
	}
	return prefix + suffix
}

func humanizeStage(stage string) string {
	stage = strings.TrimSpace(stage)
	if stage == "" {
		return ""
	}
	alias := strings.ToLower(stage)
	if strings.HasPrefix(alias, "tier") {
		numStr := strings.TrimPrefix(alias, "tier")
		if idx, err := strconv.Atoi(numStr); err == nil {
			switch idx {
			case 1:
				return "第一阶段"
			case 2:
				return "第二阶段"
			case 3:
				return "第三阶段"
			default:
				return fmt.Sprintf("第%d阶段", idx)
			}
		}
	}
	return strings.ToUpper(stage)
}

func ratioPercent(val float64) string {
	if val <= 0 {
		return ""
	}
	return formatutil.Percent(val)
}

func tierTriggered(status string, state tierComponentStateView) bool {
	if isTriggeredStatus(status) {
		return true
	}
	if state.TriggeredAt > 0 || state.ExecutedRatio > 0 {
		return true
	}
	if state.RemainingRatio > 0 && state.Ratio > 0 && state.RemainingRatio < state.Ratio {
		return true
	}
	return false
}

func isTriggeredStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "triggered", "done", "completed", "stopped", "finished":
		return true
	default:
		return false
	}
}

func normalizeStatus(values ...string) string {
	for _, v := range values {
		if s := strings.ToLower(strings.TrimSpace(v)); s != "" {
			return s
		}
	}
	return ""
}

func canonicalComponentName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func decodeRawMap(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return m
}

func fetchFloat(m map[string]any, key string) float64 {
	if len(m) == 0 {
		return 0
	}
	if val, ok := asFloat(m[key]); ok {
		return val
	}
	if snap, ok := m["original_params_snapshot"].(map[string]any); ok {
		if val, ok := asFloat(snap[key]); ok {
			return val
		}
	}
	return 0
}

func asFloat(v any) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case uint64:
		return float64(val), true
	case json.Number:
		f, err := val.Float64()
		return f, err == nil
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(val), 64); err == nil {
			return f, true
		}
	}
	return 0, false
}
