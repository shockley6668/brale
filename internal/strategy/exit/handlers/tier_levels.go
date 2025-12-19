package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"brale/internal/gateway/database"
	"brale/internal/strategy/exit"
)

const (
	ratioTolerance  = 1e-6
	maxRelativeMove = 0.50
	minTierDeltaPct = 0.002
)

func RegisterCoreHandlers(reg *exit.HandlerRegistry) {
	if reg == nil {
		return
	}
	reg.Register(newTierLevelsHandler("tier_take_profit", "take_profit"))
	reg.Register(newTierLevelsHandler("tier_stop_loss", "stop_loss"))
	reg.Register(&trailingStopHandler{})
	reg.Register(&atrTrailingHandler{base: trailingStopHandler{}})
	reg.Register(newComboHandler(reg))
}

type tierLevelsHandler struct {
	id   string
	mode string
}

func newTierLevelsHandler(id, mode string) exit.PlanHandler {
	return &tierLevelsHandler{id: id, mode: mode}
}

func (h *tierLevelsHandler) ID() string { return h.id }

func (h *tierLevelsHandler) Validate(params map[string]any) error {
	tiers, err := parseTierEntries(params["tiers"])
	if err != nil {
		return err
	}
	if len(tiers) == 0 {
		return fmt.Errorf("tiers 至少需要 1 段")
	}
	sumRatio := 0.0
	for i, tier := range tiers {
		if tier.TargetPrice <= 0 {
			return fmt.Errorf("tier#%d target_price 必须大于 0", i+1)
		}
		if tier.Ratio <= 0 || tier.Ratio > 1 {
			return fmt.Errorf("tier#%d ratio 需位于 (0,1]", i+1)
		}
		sumRatio += tier.Ratio
	}
	if math.Abs(sumRatio-1) > ratioTolerance {
		return fmt.Errorf("tiers 比例和需为 1.0，当前 %.4f", sumRatio)
	}
	return nil
}

func (h *tierLevelsHandler) Instantiate(ctx context.Context, args exit.InstantiateArgs) ([]exit.PlanInstance, error) {
	if err := h.Validate(args.PlanSpec); err != nil {
		return nil, err
	}
	entry := args.EntryPrice
	if entry <= 0 {
		return nil, fmt.Errorf("%s: entry_price 必填", h.id)
	}
	side := normalizeSide(args.Side)
	if side == "" {
		return nil, fmt.Errorf("%s: side 必填", h.id)
	}
	symbol := strings.ToUpper(strings.TrimSpace(args.Symbol))
	if symbol == "" {
		symbol = strings.ToUpper(strings.TrimSpace(args.Decision.Symbol))
	}
	tiers, err := parseTierEntries(args.PlanSpec["tiers"])
	if err != nil {
		return nil, err
	}
	if err := h.validateTargets(entry, side, tiers); err != nil {
		return nil, err
	}
	now := time.Now()
	rootState := exit.TierPlanState{
		Symbol:         symbol,
		Side:           side,
		EntryPrice:     entry,
		RemainingRatio: 1,
		LastUpdatedAt:  now.Unix(),
		LastEvent:      "",
	}
	rootPlan := cloneMap(args.PlanSpec)
	rootPlan["mode"] = h.mode
	rootState.LastEvent = ""
	root := exit.PlanInstance{
		Record: database.StrategyInstanceRecord{
			TradeID:         args.TradeID,
			PlanID:          args.PlanID,
			PlanComponent:   "",
			PlanVersion:     normalizePlanVersion(args.PlanVersion),
			ParamsJSON:      database.EncodeParams(rootPlan),
			StateJSON:       exit.EncodeTierPlanState(rootState),
			Status:          database.StrategyStatusWaiting,
			DecisionTraceID: strings.TrimSpace(args.DecisionTrace),
			CreatedAt:       now,
			UpdatedAt:       now,
		},
		Plan:  rootPlan,
		State: map[string]any{},
	}
	var instances []exit.PlanInstance
	instances = append(instances, root)
	for idx, tier := range tiers {
		component := fmt.Sprintf("tier%d", idx+1)
		state := exit.TierComponentState{
			Name:           component,
			TargetPrice:    tier.TargetPrice,
			Ratio:          tier.Ratio,
			Status:         "waiting",
			Symbol:         symbol,
			Side:           side,
			EntryPrice:     entry,
			RemainingRatio: tier.Ratio,
			Mode:           h.mode,
		}
		rec := database.StrategyInstanceRecord{
			TradeID:         args.TradeID,
			PlanID:          args.PlanID,
			PlanComponent:   component,
			PlanVersion:     normalizePlanVersion(args.PlanVersion),
			ParamsJSON:      database.EncodeParams(map[string]any{"tier": idx + 1}),
			StateJSON:       exit.EncodeTierComponentState(state),
			Status:          database.StrategyStatusWaiting,
			DecisionTraceID: strings.TrimSpace(args.DecisionTrace),
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		instances = append(instances, exit.PlanInstance{Record: rec})
	}
	return instances, nil
}

func (h *tierLevelsHandler) OnPrice(ctx context.Context, inst exit.PlanInstance, price float64) (*exit.PlanEvent, error) {
	if price <= 0 {
		return nil, nil
	}
	component := strings.TrimSpace(inst.Record.PlanComponent)
	if component == "" {
		return nil, nil
	}
	state, err := exit.DecodeTierComponentState(inst.Record.StateJSON)
	if err != nil {
		return nil, fmt.Errorf("%s: 解析组件状态失败: %w", h.id, err)
	}
	if strings.EqualFold(state.Status, "done") || strings.EqualFold(state.Status, "triggered") || strings.EqualFold(state.Status, "pending") {
		return nil, nil
	}
	side := normalizeSide(state.Side)
	if side == "" {
		return nil, nil
	}
	mode := effectiveMode(state.Mode, h.mode)
	targetHit := false
	switch mode {
	case "stop_loss":
		targetHit = hitStopLoss(side, price, state.TargetPrice)
	default:
		targetHit = tierTargetHit(side, price, state.TargetPrice)
	}
	if !targetHit {
		return nil, nil
	}
	details := map[string]any{
		"symbol":       strings.ToUpper(strings.TrimSpace(state.Symbol)),
		"side":         side,
		"target_price": state.TargetPrice,
		"ratio":        state.Ratio,
		"price":        price,
		"component":    inst.Record.PlanComponent,
		"mode":         mode,
	}
	return &exit.PlanEvent{
		TradeID:       inst.Record.TradeID,
		PlanID:        inst.Record.PlanID,
		PlanComponent: inst.Record.PlanComponent,
		Type:          exit.PlanEventTypeTierHit,
		Details:       details,
	}, nil
}

func (h *tierLevelsHandler) OnAdjust(ctx context.Context, inst exit.PlanInstance, params map[string]any) (*exit.PlanEvent, error) {
	component := strings.TrimSpace(inst.Record.PlanComponent)
	if component == "" {
		return nil, fmt.Errorf("%s: 根组件不支持调整", h.id)
	}
	state, err := exit.DecodeTierComponentState(inst.Record.StateJSON)
	if err != nil {
		return nil, fmt.Errorf("%s: 解析组件状态失败: %w", h.id, err)
	}
	if !canAdjustComponent(state.Status) {
		return nil, fmt.Errorf("%s: 组件 %s 已完成，无法调整", h.id, component)
	}
	changes := make(map[string]any)
	if price, ok := number(params["target_price"]); ok && price > 0 {
		state.TargetPrice = price
		changes["target_price"] = price
	} else if pct, ok := number(params["target"]); ok && pct > 0 {

		state.TargetPrice = state.EntryPrice * (1 + pct)
		changes["target_price"] = state.TargetPrice
	}
	if ratio, ok := number(params["ratio"]); ok && ratio > 0 && ratio <= 1 {
		state.Ratio = ratio
		state.RemainingRatio = ratio
		changes["ratio"] = ratio
	}
	if len(changes) == 0 {
		return nil, nil
	}
	state.Status = "waiting"
	state.PendingOrderID = ""
	state.PendingSince = 0
	state.LastEvent = exit.PlanEventTypeAdjust
	stateJSON := exit.EncodeTierComponentState(state)
	details := map[string]any{
		"component":  component,
		"changes":    changes,
		"state_json": stateJSON,
	}
	return &exit.PlanEvent{
		TradeID:       inst.Record.TradeID,
		PlanID:        inst.Record.PlanID,
		PlanComponent: component,
		Type:          exit.PlanEventTypeAdjust,
		Details:       details,
	}, nil
}

type tierEntry struct {
	TargetPrice float64
	Ratio       float64
}

func parseTierEntries(raw interface{}) ([]tierEntry, error) {
	items, ok := raw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("tiers 参数无效")
	}
	var tiers []tierEntry
	for idx, item := range items {
		source, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("tier#%d 参数格式错误", idx+1)
		}
		target, ok := numberFromKeys(source, "target_price", "target")
		if !ok || target <= 0 {
			return nil, fmt.Errorf("tier#%d 缺少 target_price", idx+1)
		}
		ratio, ok := number(source["ratio"])
		if !ok || ratio <= 0 || ratio > 1 {
			return nil, fmt.Errorf("tier#%d ratio 非法", idx+1)
		}
		tiers = append(tiers, tierEntry{TargetPrice: target, Ratio: ratio})
	}
	return tiers, nil
}

func (h *tierLevelsHandler) validateTargets(entry float64, side string, tiers []tierEntry) error {
	if entry <= 0 {
		return fmt.Errorf("entry_price 必须 >0")
	}
	expectAsc := h.expectAscending(side)
	prev := 0.0
	for idx, tier := range tiers {
		diff := tier.TargetPrice - entry
		if !isDirectionValid(h.mode, side, diff) {
			return fmt.Errorf("tier#%d 目标价 %.4f 不符合 %s 方向", idx+1, tier.TargetPrice, h.mode)
		}
		movePct := math.Abs(diff / entry)
		if movePct > maxRelativeMove {
			return fmt.Errorf("tier#%d 目标距离过大 (%.2f%%)，上限 %.0f%%", idx+1, movePct*100, maxRelativeMove*100)
		}
		if idx > 0 {
			step := tier.TargetPrice - prev
			stepPct := math.Abs(step / entry)
			if stepPct < minTierDeltaPct {
				return fmt.Errorf("tier#%d 与前一段的间隔过小 (<%.2f%%)", idx+1, minTierDeltaPct*100)
			}
			if expectAsc && step <= 0 {
				return fmt.Errorf("tier#%d 必须严格递增", idx+1)
			}
			if !expectAsc && step >= 0 {
				return fmt.Errorf("tier#%d 必须严格递减", idx+1)
			}
		} else {

			if expectAsc && diff <= 0 {
				return fmt.Errorf("tier#%d 需高于开仓价 %.4f", idx+1, entry)
			}
			if !expectAsc && diff >= 0 {
				return fmt.Errorf("tier#%d 需低于开仓价 %.4f", idx+1, entry)
			}
		}
		prev = tier.TargetPrice
	}
	return nil
}

func (h *tierLevelsHandler) expectAscending(side string) bool {
	switch h.mode {
	case "stop_loss":
		return strings.EqualFold(side, "short")
	default:
		return !strings.EqualFold(side, "short")
	}
}

func isDirectionValid(mode, side string, diff float64) bool {
	switch mode {
	case "stop_loss":
		if strings.EqualFold(side, "short") {
			return diff > 0
		}
		return diff < 0
	default:
		if strings.EqualFold(side, "short") {
			return diff < 0
		}
		return diff > 0
	}
}

func effectiveMode(componentMode, handlerMode string) string {
	mode := strings.TrimSpace(componentMode)
	if mode != "" {
		return strings.ToLower(mode)
	}
	return strings.ToLower(handlerMode)
}

func number(v interface{}) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case json.Number:
		f, err := val.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(val), 64)
		return f, err == nil
	default:
		return 0, false
	}
}

func numberFromKeys(m map[string]any, keys ...string) (float64, bool) {
	if len(keys) == 0 || len(m) == 0 {
		return 0, false
	}
	for _, key := range keys {
		if key == "" {
			continue
		}
		if val, ok := m[key]; ok {
			if num, ok := number(val); ok {
				return num, true
			}
		}
	}
	return 0, false
}

func normalizeSide(side string) string {
	return strings.ToLower(strings.TrimSpace(side))
}

func cloneMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return map[string]any{}
	}
	dup := make(map[string]any, len(src))
	for k, v := range src {
		dup[k] = v
	}
	return dup
}

func canAdjustComponent(status string) bool {
	status = strings.ToLower(strings.TrimSpace(status))
	switch status {
	case "", "waiting":
		return true
	default:
		return false
	}
}
