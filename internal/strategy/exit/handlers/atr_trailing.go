package handlers

import (
	"context"
	"fmt"

	"brale/internal/strategy/exit"
)

const (
	minATRTriggerMultiplier = 1.0
	maxATRTriggerMultiplier = 5.0
	minATRTrailMultiplier   = 0.5
)

type atrTrailingHandler struct {
	base trailingStopHandler
}

func (h *atrTrailingHandler) ID() string { return "atr_trailing" }

func (h *atrTrailingHandler) Validate(params map[string]any) error {
	if err := validateModeParam(params); err != nil {
		return err
	}
	if err := validateATRTrailingParams(params); err != nil {
		return err
	}
	initialStopMult, hasStop := number(params["initial_stop_multiplier"])
	if hasStop && initialStopMult < 0 {
		return fmt.Errorf("initial_stop_multiplier 不可为负")
	}
	if hasStop && initialStopMult > 0 && initialStopMult < 1 {
		return fmt.Errorf("initial_stop_multiplier 需 >= 1 或省略（等价于无起始止损）")
	}
	return nil
}

func validateATRTrailingParams(params map[string]any) error {
	atr, ok := number(params["atr_value"])
	if !ok || atr <= 0 {
		return fmt.Errorf("atr_value 需 >0")
	}
	triggerMult, ok := number(params["trigger_multiplier"])
	if !ok || triggerMult <= 0 {
		return fmt.Errorf("trigger_multiplier 需 >0")
	}
	if triggerMult < minATRTriggerMultiplier || triggerMult > maxATRTriggerMultiplier {
		return fmt.Errorf("trigger_multiplier 需位于 [%.1f, %.1f]", minATRTriggerMultiplier, maxATRTriggerMultiplier)
	}
	trailMult, ok := number(params["trail_multiplier"])
	if !ok || trailMult <= 0 {
		return fmt.Errorf("trail_multiplier 需 >0")
	}
	if trailMult < minATRTrailMultiplier {
		return fmt.Errorf("trail_multiplier 需 >= %.1f", minATRTrailMultiplier)
	}
	if trailMult >= triggerMult {
		return fmt.Errorf("trail_multiplier 需小于 trigger_multiplier")
	}
	return nil
}

func (h *atrTrailingHandler) Instantiate(ctx context.Context, args exit.InstantiateArgs) ([]exit.PlanInstance, error) {
	if err := h.Validate(args.PlanSpec); err != nil {
		return nil, err
	}
	entry := args.EntryPrice
	if entry <= 0 {
		return nil, fmt.Errorf("atr_trailing: entry_price 必填")
	}
	mode := ""
	if raw, ok := args.PlanSpec["mode"]; ok {
		if val, ok := raw.(string); ok {
			mode = val
		}
	}
	mode = effectiveMode(mode, "take_profit")
	atr, _ := number(args.PlanSpec["atr_value"])
	triggerMul, _ := number(args.PlanSpec["trigger_multiplier"])
	trailMul, _ := number(args.PlanSpec["trail_multiplier"])
	initialStopMul, hasInitial := number(args.PlanSpec["initial_stop_multiplier"])
	triggerPct := (atr * triggerMul) / entry
	trailPct := (atr * trailMul) / entry
	if triggerPct <= 0 || trailPct <= 0 {
		return nil, fmt.Errorf("atr_trailing: 触发/追踪比例需 >0，请检查 ATR 与 multiplier 是否合理")
	}
	derived := map[string]any{
		"atr_value":                atr,
		"trigger_multiplier":       triggerMul,
		"trail_multiplier":         trailMul,
		"trigger_pct":              triggerPct,
		"trail_pct":                trailPct,
		"mode":                     mode,
		"initial_stop_multiplier":  initialStopMul,
		"original_params_snapshot": cloneMap(args.PlanSpec),
	}
	if hasInitial && initialStopMul > 0 {
		derived["initial_stop_pct"] = (atr * initialStopMul) / entry
	}
	clone := args
	clone.PlanSpec = derived
	return h.base.Instantiate(ctx, clone)
}

func (h *atrTrailingHandler) OnPrice(ctx context.Context, inst exit.PlanInstance, price float64) (*exit.PlanEvent, error) {
	return h.base.OnPrice(ctx, inst, price)
}

func (h *atrTrailingHandler) OnAdjust(ctx context.Context, inst exit.PlanInstance, params map[string]any) (*exit.PlanEvent, error) {
	mapped := make(map[string]any, len(params))
	for k, v := range params {
		mapped[k] = v
	}
	if pct, ok := h.maybeConvertToPct(inst, mapped, "trigger_multiplier"); ok {
		mapped["trigger_pct"] = pct
	}
	if pct, ok := h.maybeConvertToPct(inst, mapped, "trail_multiplier"); ok {
		mapped["trail_pct"] = pct
	}
	delete(mapped, "trigger_multiplier")
	delete(mapped, "trail_multiplier")
	return h.base.OnAdjust(ctx, inst, mapped)
}

func (h *atrTrailingHandler) maybeConvertToPct(inst exit.PlanInstance, params map[string]any, key string) (float64, bool) {
	raw, ok := params[key]
	if !ok {
		return 0, false
	}
	mul, ok := number(raw)
	if !ok {
		return 0, false
	}
	if mul <= 0 {
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

func extractATR(plan map[string]any) float64 {
	if len(plan) == 0 {
		return 0
	}
	if v, ok := plan["atr_value"]; ok {
		if val, ok := number(v); ok {
			return val
		}
	}
	if snap, ok := plan["original_params_snapshot"].(map[string]any); ok {
		if val, ok := number(snap["atr_value"]); ok {
			return val
		}
	}
	return 0
}
