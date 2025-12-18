package exit

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"brale/internal/gateway/database"
)

// Summaries 将 strategy_instances 记录转换为可读文本。
func Summaries(recs []database.StrategyInstanceRecord) []string {
	out := make([]string, 0, len(recs))
	for _, rec := range recs {
		if msg := summarizeTierLike(rec); msg != "" {
			out = append(out, msg)
			continue
		}
		component := strings.TrimSpace(rec.PlanComponent)
		if component == "" {
			component = "root"
		}
		status := StatusLabel(rec.Status)
		state := trimJSON(rec.StateJSON, 120)
		out = append(out, rec.PlanID+"/"+component+" v"+intToString(rec.PlanVersion)+" ["+status+"] state="+state)
	}
	return out
}

func summarizeTierLike(rec database.StrategyInstanceRecord) string {
	component := strings.TrimSpace(rec.PlanComponent)
	status := StatusLabel(rec.Status)
	if component == "" {
		return summarizeTierRoot(rec, status)
	}
	if strings.Contains(component, "tier") {
		return summarizeTierComponent(rec, status)
	}
	return ""
}

func summarizeTierRoot(rec database.StrategyInstanceRecord, status string) string {
	state, err := DecodeTierPlanState(rec.StateJSON)
	if err != nil {
		return ""
	}
	if strings.TrimSpace(state.Symbol) == "" && state.EntryPrice == 0 {
		return ""
	}
	parts := []string{rec.PlanID + "/ROOT", "status=" + firstNonEmpty(state.LastEvent, status)}
	if mode := strings.TrimSpace(state.Mode); mode != "" {
		parts = append(parts, "mode="+mode)
	}
	if state.RemainingRatio > 0 {
		parts = append(parts, "remain="+formatPercent(state.RemainingRatio))
	}
	if state.PendingEvent != "" {
		ts := "pending"
		if state.PendingSince > 0 {
			ts = time.Unix(state.PendingSince, 0).Format(time.RFC3339)
		}
		parts = append(parts, "pending="+state.PendingEvent+"@"+ts)
	}
	if state.TakeProfitPrice > 0 {
		parts = append(parts, "tp="+formatFloat(state.TakeProfitPrice))
	}
	if state.StopLossPrice > 0 {
		parts = append(parts, "sl="+formatFloat(state.StopLossPrice))
	}
	return strings.Join(parts, " ")
}

func summarizeTierComponent(rec database.StrategyInstanceRecord, status string) string {
	state, err := DecodeTierComponentState(rec.StateJSON)
	if err != nil {
		return ""
	}
	if state.TargetPrice <= 0 {
		return ""
	}
	label := firstNonEmpty(state.LastEvent, state.Status, status)
	parts := []string{
		rec.PlanID + "/" + strings.ToUpper(strings.TrimSpace(rec.PlanComponent)),
		"target=" + formatFloat(state.TargetPrice),
		"ratio=" + formatPercent(state.Ratio),
		"status=" + label,
	}
	if mode := strings.TrimSpace(state.Mode); mode != "" {
		parts = append(parts, "mode="+mode)
	}
	if state.PendingOrderID != "" {
		parts = append(parts, "order="+state.PendingOrderID)
	}
	if state.PendingSince > 0 {
		parts = append(parts, "since="+time.Unix(state.PendingSince, 0).Format(time.RFC3339))
	}
	if state.TriggerPrice > 0 {
		parts = append(parts, "hit="+formatFloat(state.TriggerPrice))
	}
	return strings.Join(parts, " ")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// StatusLabel 将 StrategyStatus 映射为可读文本。
func StatusLabel(st database.StrategyStatus) string {
	switch st {
	case database.StrategyStatusPending:
		return "pending"
	case database.StrategyStatusDone:
		return "done"
	case database.StrategyStatusPaused:
		return "paused"
	default:
		return "waiting"
	}
}

func trimJSON(raw string, max int) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "-"
	}
	if len(raw) <= max {
		return raw
	}
	if max <= 3 {
		return raw[:max]
	}
	return raw[:max-3] + "..."
}

func formatFloat(val float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.4f", val), "0"), ".")
}

func formatPercent(val float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f%%", val*100), "0"), ".")
}

func intToString(v int) string {
	return strconv.Itoa(v)
}
