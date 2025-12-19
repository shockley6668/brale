package handlers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"brale/internal/gateway/database"
	"brale/internal/strategy/exit"
)

type trailingStopHandler struct{}

func (h *trailingStopHandler) ID() string { return "trailing_stop_pct" }

func (h *trailingStopHandler) Validate(params map[string]any) error {
	if err := validateModeParam(params); err != nil {
		return err
	}
	trigger, okTrig := number(params["trigger_pct"])
	trail, okTrail := number(params["trail_pct"])
	if okTrig && okTrail {
		return validateTrailingPctParams(trigger, trail)
	}
	return validateTrailingATRParams(params)
}

func (h *trailingStopHandler) Instantiate(ctx context.Context, args exit.InstantiateArgs) ([]exit.PlanInstance, error) {
	if err := h.Validate(args.PlanSpec); err != nil {
		return nil, err
	}
	entry := args.EntryPrice
	if entry <= 0 {
		return nil, fmt.Errorf("trailing_stop_pct: entry_price 必填")
	}
	side := normalizeSide(args.Side)
	if side == "" {
		return nil, fmt.Errorf("trailing_stop_pct: side 必填")
	}
	symbol := strings.ToUpper(strings.TrimSpace(args.Symbol))
	if symbol == "" {
		symbol = strings.ToUpper(strings.TrimSpace(args.Decision.Symbol))
	}
	mode := ""
	if raw, ok := args.PlanSpec["mode"]; ok {
		if val, ok := raw.(string); ok {
			mode = val
		}
	}
	mode = effectiveMode(mode, "take_profit")
	triggerPct, trailPct, initialStopPct, err := h.resolveTrailingPercents(entry, args.PlanSpec)
	if err != nil {
		return nil, err
	}
	derivedPlan := cloneMap(args.PlanSpec)
	derivedPlan["trigger_pct"] = triggerPct
	derivedPlan["trail_pct"] = trailPct
	derivedPlan["mode"] = mode
	if initialStopPct > 0 {
		derivedPlan["initial_stop_pct"] = initialStopPct
	}
	now := time.Now()
	state := exit.TierPlanState{
		Symbol:                  symbol,
		Side:                    side,
		EntryPrice:              entry,
		RemainingRatio:          1,
		TrailingPeakPrice:       entry,
		TrailingTroughPrice:     entry,
		TrailingActivationPrice: relativeTarget(entry, triggerPct, side),
		TriggerPct:              triggerPct,
		TrailPct:                trailPct,
		Mode:                    mode,
		LastUpdatedAt:           now.Unix(),
	}
	if initialStopPct > 0 {
		state.StopLossPrice = relativePrice(entry, -initialStopPct, side)
		state.TrailingStopPrice = state.StopLossPrice
	}
	rec := database.StrategyInstanceRecord{
		TradeID:         args.TradeID,
		PlanID:          args.PlanID,
		PlanComponent:   "",
		PlanVersion:     normalizePlanVersion(args.PlanVersion),
		ParamsJSON:      database.EncodeParams(derivedPlan),
		StateJSON:       exit.EncodeTierPlanState(state),
		Status:          database.StrategyStatusWaiting,
		DecisionTraceID: strings.TrimSpace(args.DecisionTrace),
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	inst := exit.PlanInstance{
		Record: rec,
		Plan:   derivedPlan,
		State:  map[string]any{},
	}
	return []exit.PlanInstance{inst}, nil
}

func (h *trailingStopHandler) OnPrice(ctx context.Context, inst exit.PlanInstance, price float64) (*exit.PlanEvent, error) {
	if price <= 0 {
		return nil, nil
	}
	state, err := exit.DecodeTierPlanState(inst.Record.StateJSON)
	if err != nil {
		return nil, fmt.Errorf("trailing_stop_pct: 解析状态失败: %w", err)
	}
	mode := strings.TrimSpace(state.Mode)
	if mode == "" {
		if raw, ok := inst.Plan["mode"].(string); ok {
			mode = raw
		}
	}
	mode = effectiveMode(mode, "take_profit")
	side := normalizeSide(state.Side)
	if side == "" {
		return nil, nil
	}
	updated := false
	if !state.TrailingActive && state.TrailingActivationPrice > 0 {
		if activationHit(side, price, state.TrailingActivationPrice) {
			state.TrailingActive = true
			state.TrailingPeakPrice = price
			state.TrailingTroughPrice = price
			state.TrailingStopPrice = trailingStopFor(side, price, state.TrailPct)
			state.StopLossPrice = state.TrailingStopPrice
			state.LastEvent = "trailing_activate"
			updated = true
		}
	} else if state.TrailingActive {
		anchor := state.TrailingPeakPrice
		if side == "short" {
			anchor = state.TrailingTroughPrice
		}
		if shouldUpdateAnchor(side, price, anchor) {
			if side == "short" {
				state.TrailingTroughPrice = price
			} else {
				state.TrailingPeakPrice = price
			}
			newStop := trailingStopFor(side, price, state.TrailPct)
			if shouldUpdateStop(side, newStop, state.TrailingStopPrice) {
				state.TrailingStopPrice = newStop
				state.StopLossPrice = newStop
				state.LastEvent = "trailing_adjust"
				updated = true
			}
		}
		if priceBreachedStop(side, price, state.TrailingStopPrice) {
			evtType := exit.PlanEventTypeFinalTakeProfit
			if mode == "stop_loss" {
				evtType = exit.PlanEventTypeFinalStopLoss
			}
			details := map[string]any{
				"symbol":       state.Symbol,
				"side":         side,
				"target":       state.TrailingStopPrice,
				"price":        price,
				"mode":         mode,
				"trigger_kind": "trailing_stop",
			}
			return &exit.PlanEvent{
				TradeID: inst.Record.TradeID,
				PlanID:  inst.Record.PlanID,
				Type:    evtType,
				Details: details,
			}, nil
		}
	}
	if updated {
		state.LastUpdatedAt = time.Now().Unix()
		return buildAdjustEvent(inst, state)
	}
	return nil, nil
}

func (h *trailingStopHandler) OnAdjust(ctx context.Context, inst exit.PlanInstance, params map[string]any) (*exit.PlanEvent, error) {
	if len(params) == 0 {
		return nil, nil
	}
	state, err := exit.DecodeTierPlanState(inst.Record.StateJSON)
	if err != nil {
		return nil, fmt.Errorf("trailing_stop_pct: 解析状态失败: %w", err)
	}
	mapped := cloneMap(params)
	if pct, ok := h.maybeConvertMultiplier(inst, mapped, "trigger_multiplier"); ok {
		mapped["trigger_pct"] = pct
	}
	if pct, ok := h.maybeConvertMultiplier(inst, mapped, "trail_multiplier"); ok {
		mapped["trail_pct"] = pct
	}

	nextTriggerPct := state.TriggerPct
	nextTrailPct := state.TrailPct
	triggerChanged := false
	trailChanged := false

	if pct, ok := number(mapped["trigger_pct"]); ok && pct > 0 {
		nextTriggerPct = pct
		triggerChanged = true
	}
	if pct, ok := number(mapped["trail_pct"]); ok && pct > 0 {
		nextTrailPct = pct
		trailChanged = true
	}
	if !triggerChanged && !trailChanged {
		return nil, nil
	}

	if nextTrailPct >= nextTriggerPct {
		return nil, fmt.Errorf("trailing_stop_pct: trail_pct 需小于 trigger_pct")
	}
	if nextTriggerPct > 0.25 {
		return nil, fmt.Errorf("trailing_stop_pct: trigger_pct 不能超过 25%%，当前=%.2f%%", nextTriggerPct*100)
	}
	if nextTriggerPct < 0.005 {
		return nil, fmt.Errorf("trailing_stop_pct: trigger_pct 至少 0.5%%，当前=%.2f%%", nextTriggerPct*100)
	}
	if nextTrailPct < 0.002 {
		return nil, fmt.Errorf("trailing_stop_pct: trail_pct 至少 0.2%%，当前=%.2f%%", nextTrailPct*100)
	}

	state.TriggerPct = nextTriggerPct
	state.TrailPct = nextTrailPct
	if triggerChanged {
		state.TrailingActivationPrice = relativeTarget(state.EntryPrice, nextTriggerPct, state.Side)
	}
	if trailChanged && state.TrailingActive {
		anchor := state.TrailingPeakPrice
		if strings.EqualFold(state.Side, "short") {
			anchor = state.TrailingTroughPrice
		}
		state.TrailingStopPrice = trailingStopFor(state.Side, anchor, nextTrailPct)
		state.StopLossPrice = state.TrailingStopPrice
	}
	state.LastUpdatedAt = time.Now().Unix()
	return buildAdjustEvent(inst, state)
}

func buildAdjustEvent(inst exit.PlanInstance, state exit.TierPlanState) (*exit.PlanEvent, error) {
	next := exit.PlanEvent{
		TradeID:       inst.Record.TradeID,
		PlanID:        inst.Record.PlanID,
		PlanComponent: inst.Record.PlanComponent,
		Type:          exit.PlanEventTypeAdjust,
		Details: map[string]any{
			"component":  inst.Record.PlanComponent,
			"state_json": exit.EncodeTierPlanState(state),
		},
	}
	return &next, nil
}

func (h *trailingStopHandler) resolveTrailingPercents(entry float64, params map[string]any) (float64, float64, float64, error) {
	triggerPct, okTrig := number(params["trigger_pct"])
	trailPct, okTrail := number(params["trail_pct"])
	initialStopPct, _ := number(params["initial_stop_pct"])
	if okTrig && okTrail && triggerPct > 0 && trailPct > 0 {
		if err := validateTrailingPctParams(triggerPct, trailPct); err != nil {
			return 0, 0, 0, fmt.Errorf("trailing_stop_pct: %w", err)
		}
		return triggerPct, trailPct, initialStopPct, nil
	}
	atrVal, okATR := number(params["atr_value"])
	triggerMul, okTrigMul := number(params["trigger_multiplier"])
	trailMul, okTrailMul := number(params["trail_multiplier"])
	if !okATR || !okTrigMul || !okTrailMul || atrVal <= 0 || triggerMul <= 0 || trailMul <= 0 {
		return 0, 0, 0, fmt.Errorf("trailing_stop_pct: 缺少 trigger_pct/trail_pct，且未提供 ATR 参数")
	}
	if entry <= 0 {
		return 0, 0, 0, fmt.Errorf("trailing_stop_pct: entry_price 必填以计算 ATR 百分比")
	}
	triggerPct = (atrVal * triggerMul) / entry
	trailPct = (atrVal * trailMul) / entry
	if triggerPct <= 0 || trailPct <= 0 {
		return 0, 0, 0, fmt.Errorf("trailing_stop_pct: 根据 ATR 计算的 trigger_pct/trail_pct 无效")
	}
	if trailPct >= triggerPct {
		return 0, 0, 0, fmt.Errorf("trailing_stop_pct: trail_multiplier 需小于 trigger_multiplier")
	}
	if initMul, ok := number(params["initial_stop_multiplier"]); ok && initMul > 0 {
		initialStopPct = (atrVal * initMul) / entry
	}
	return triggerPct, trailPct, initialStopPct, nil
}

func (h *trailingStopHandler) maybeConvertMultiplier(inst exit.PlanInstance, params map[string]any, key string) (float64, bool) {
	if val, ok := params[key]; ok {
		mul, ok := number(val)
		if !ok || mul <= 0 {
			return 0, false
		}
		atr := extractATR(inst.Plan)
		if atr <= 0 {
			return 0, false
		}
		state, err := exit.DecodeTierPlanState(inst.Record.StateJSON)
		if err != nil || state.EntryPrice <= 0 {
			return 0, false
		}
		return (atr * mul) / state.EntryPrice, true
	}
	return 0, false
}
