package exit

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"brale/internal/decision"
	"brale/internal/gateway/database"
)

type BuildArgs struct {
	TradeID       int
	TraceID       string
	Symbol        string
	PlanSpec      *decision.ExitPlanSpec
	PlanVersion   int
	Component     string
	Decision      decision.Decision
	EntryPrice    float64
	Side          string
	ComponentSeed string
}

type TierPlanState struct {
	Symbol                   string  `json:"symbol"`
	Side                     string  `json:"side"`
	EntryPrice               float64 `json:"entry_price"`
	StopLossPrice            float64 `json:"stop_loss"`
	TakeProfitPrice          float64 `json:"take_profit"`
	FinalTakeProfit          float64 `json:"final_take_profit,omitempty"`
	FinalStopLoss            float64 `json:"final_stop_loss,omitempty"`
	LastUpdatedAt            int64   `json:"updated_at,omitempty"`
	AllocatedPercent         float64 `json:"allocated_percent,omitempty"`
	StopLossTriggered        bool    `json:"stop_loss_triggered,omitempty"`
	TakeProfitTriggered      bool    `json:"take_profit_triggered,omitempty"`
	FinalStopLossTriggered   bool    `json:"final_stop_loss_triggered,omitempty"`
	FinalTakeProfitTriggered bool    `json:"final_take_profit_triggered,omitempty"`
	RemainingRatio           float64 `json:"remaining_ratio,omitempty"`
	PendingEvent             string  `json:"pending_event,omitempty"`
	PendingOrderID           string  `json:"pending_order_id,omitempty"`
	PendingSince             int64   `json:"pending_since,omitempty"`
	LastEvent                string  `json:"last_event,omitempty"`
	TrailingActive           bool    `json:"trailing_active,omitempty"`
	TrailingPeakPrice        float64 `json:"trailing_peak_price,omitempty"`
	TrailingTroughPrice      float64 `json:"trailing_trough_price,omitempty"`
	TrailingActivationPrice  float64 `json:"trailing_activation_price,omitempty"`
	TrailingStopPrice        float64 `json:"trailing_stop_price,omitempty"`
	TriggerPct               float64 `json:"trigger_pct,omitempty"`
	TrailPct                 float64 `json:"trail_pct,omitempty"`
	Mode                     string  `json:"mode,omitempty"`
}

type TierComponentState struct {
	Name           string  `json:"name"`
	TargetPrice    float64 `json:"target_price"`
	Ratio          float64 `json:"ratio"`
	Status         string  `json:"status"`
	TriggeredAt    int64   `json:"triggered_at,omitempty"`
	TriggerPrice   float64 `json:"trigger_price,omitempty"`
	Symbol         string  `json:"symbol,omitempty"`
	Side           string  `json:"side,omitempty"`
	EntryPrice     float64 `json:"entry_price,omitempty"`
	PendingOrderID string  `json:"pending_order_id,omitempty"`
	PendingSince   int64   `json:"pending_since,omitempty"`
	ExecutedRatio  float64 `json:"executed_ratio,omitempty"`
	RemainingRatio float64 `json:"remaining_ratio,omitempty"`
	LastEvent      string  `json:"last_event,omitempty"`
	Mode           string  `json:"mode,omitempty"`
}

func BuildStrategyInstanceRecords(args BuildArgs) []database.StrategyInstanceRecord {
	if args.PlanSpec == nil || strings.TrimSpace(args.PlanSpec.ID) == "" {
		return nil
	}
	planID := strings.TrimSpace(args.PlanSpec.ID)
	component := strings.TrimSpace(args.Component)
	if component == "" {
		component = strings.TrimSpace(args.ComponentSeed)
	}
	now := time.Now()
	rec := database.StrategyInstanceRecord{
		TradeID:         args.TradeID,
		PlanID:          planID,
		PlanComponent:   component,
		PlanVersion:     normalizeVersion(args.PlanVersion),
		ParamsJSON:      database.EncodeParams(args.PlanSpec.Params),
		StateJSON:       "{}",
		Status:          database.StrategyStatusWaiting,
		DecisionTraceID: strings.TrimSpace(args.TraceID),
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	records := []database.StrategyInstanceRecord{rec}
	for idx := range args.PlanSpec.Components {
		child := args.PlanSpec.Components[idx]
		childID := strings.TrimSpace(child.ID)
		if childID == "" {
			childID = fmt.Sprintf("component_%d", idx+1)
		}
		childArgs := args
		childArgs.PlanSpec = &child
		childArgs.Component = childID
		sub := BuildStrategyInstanceRecords(childArgs)
		if len(sub) > 0 {
			records = append(records, sub...)
		}
	}
	return records
}

func normalizeVersion(v int) int {
	if v <= 0 {
		return 1
	}
	return v
}

func encodeState(v interface{}) string {
	buf, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(buf)
}

func DecodeTierComponentState(raw string) (TierComponentState, error) {
	state := TierComponentState{}
	if strings.TrimSpace(raw) == "" {
		return state, nil
	}
	err := json.Unmarshal([]byte(raw), &state)
	return state, err
}

func DecodeTierPlanState(raw string) (TierPlanState, error) {
	state := TierPlanState{}
	if strings.TrimSpace(raw) == "" {
		return state, nil
	}
	err := json.Unmarshal([]byte(raw), &state)
	return state, err
}

func EncodeTierComponentState(state TierComponentState) string {
	return encodeState(state)
}

func EncodeTierPlanState(state TierPlanState) string {
	return encodeState(state)
}
