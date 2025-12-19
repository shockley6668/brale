package freqtrade

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"brale/internal/decision"
	"brale/internal/gateway/database"
	"brale/internal/gateway/exchange"
	"brale/internal/logger"
	"brale/internal/strategy/exit"
	"brale/internal/trader"
)

func snapshotDecisionPositions(state *trader.State) []decision.PositionSnapshot {
	if state == nil || len(state.Positions) == 0 {
		return nil
	}
	now := time.Now()
	out := make([]decision.PositionSnapshot, 0, len(state.Positions))
	for _, pos := range state.Positions {
		holdingMs := int64(0)
		if !pos.UpdatedAt.IsZero() {
			holdingMs = now.Sub(pos.UpdatedAt).Milliseconds()
		}
		tradeKey := ""
		if state.SymbolIndex != nil {
			tradeKey = state.SymbolIndex[strings.ToUpper(pos.Symbol)]
		}
		planJSON := buildPlanStateJSON(state.Plans[tradeKey])
		out = append(out, decision.PositionSnapshot{
			Symbol:        pos.Symbol,
			Side:          pos.Side,
			Quantity:      pos.Amount,
			EntryPrice:    pos.EntryPrice,
			CurrentPrice:  pos.EntryPrice,
			HoldingMs:     holdingMs,
			PlanStateJSON: planJSON,
		})
	}
	return out
}

type planComponentSummary struct {
	Component string          `json:"component"`
	Status    string          `json:"status"`
	Params    json.RawMessage `json:"params,omitempty"`
	State     json.RawMessage `json:"state,omitempty"`
}

type planStateSummary struct {
	PlanID             string                 `json:"plan_id"`
	Version            int                    `json:"version"`
	Components         []planComponentSummary `json:"components"`
	EditableComponents []string               `json:"editable_components,omitempty"`
	Instruction        string                 `json:"instruction,omitempty"`
}

func buildPlanStateJSON(snaps []exit.StrategyPlanSnapshot) string {
	if len(snaps) == 0 {
		return ""
	}
	group := make(map[string]*planStateSummary)
	for _, snap := range snaps {
		planID := strings.TrimSpace(snap.PlanID)
		if planID == "" {
			planID = "unknown"
		}
		summary, ok := group[planID]
		if !ok {
			summary = &planStateSummary{
				PlanID:      planID,
				Version:     snap.PlanVersion,
				Instruction: "editable_components lists stages that haven't triggered yet.",
			}
			group[planID] = summary
		} else if snap.PlanVersion > summary.Version {
			summary.Version = snap.PlanVersion
		}
		component := strings.TrimSpace(snap.PlanComponent)
		status := strings.TrimSpace(snap.StatusLabel)
		if status == "" {
			status = exit.StatusLabel(database.StrategyStatus(snap.StatusCode))
		}
		entry := planComponentSummary{
			Component: component,
			Status:    status,
			Params:    normalizeRawJSON(snap.ParamsJSON),
			State:     normalizeRawJSON(snap.StateJSON),
		}
		summary.Components = append(summary.Components, entry)
		if !strings.EqualFold(status, "done") && component != "" {
			summary.EditableComponents = append(summary.EditableComponents, component)
		}
	}
	if len(group) == 0 {
		return ""
	}
	plans := make([]planStateSummary, 0, len(group))
	keys := make([]string, 0, len(group))
	for k := range group {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		summary := group[key]
		if len(summary.Components) > 1 {
			sort.Slice(summary.Components, func(i, j int) bool {
				return summary.Components[i].Component < summary.Components[j].Component
			})
		}
		if len(summary.EditableComponents) > 1 {
			sort.Strings(summary.EditableComponents)
		}
		plans = append(plans, *summary)
	}
	data, err := json.MarshalIndent(plans, "", "  ")
	if err != nil {
		logger.Warnf("failed to marshal plan state summary: %v", err)
		return ""
	}
	return string(data)
}

func normalizeRawJSON(raw string) json.RawMessage {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "null") {
		return nil
	}
	if json.Valid([]byte(raw)) {
		msg := json.RawMessage(raw)
		return msg
	}
	quoted, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	return json.RawMessage(quoted)
}

func snapshotTradeEvents(state *trader.State, tradeID int, limit int) []exchange.TradeEvent {
	if state == nil || tradeID <= 0 {
		return nil
	}
	key := strconv.Itoa(tradeID)
	events := state.TradeEvents[key]
	if len(events) == 0 {
		return nil
	}
	if limit > 0 && len(events) > limit {
		events = events[len(events)-limit:]
	}
	return convertToExchangeTradeEvents(events)
}

func convertToExchangeTradeEvents(recs []database.TradeOperationRecord) []exchange.TradeEvent {
	if len(recs) == 0 {
		return nil
	}
	out := make([]exchange.TradeEvent, len(recs))
	for i, rec := range recs {
		out[i] = exchange.TradeEvent{
			FreqtradeID: rec.FreqtradeID,
			Symbol:      rec.Symbol,
			Operation:   int(rec.Operation),
			Details:     rec.Details,
			Timestamp:   rec.Timestamp,
		}
	}
	return out
}
