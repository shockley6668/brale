package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"brale/internal/exitplan"
	"brale/internal/gateway/database"
	"brale/internal/logger"
	"brale/internal/pkg/utils"
	"brale/internal/strategy/exit"
)

type planWatcher struct {
	tradeID    int
	symbol     string
	side       string
	planID     string
	handler    exit.PlanHandler
	rootInst   *exit.PlanInstance
	components map[string]*exit.PlanInstance
}

type tradeOperationStore interface {
	AppendTradeOperation(ctx context.Context, op database.TradeOperationRecord) error
}

type PlanRepository struct {
	store    exit.StrategyStore
	plans    *exitplan.Registry
	handlers *exit.HandlerRegistry
}

func NewPlanRepository(store exit.StrategyStore, plans *exitplan.Registry, handlers *exit.HandlerRegistry) *PlanRepository {
	return &PlanRepository{
		store:    store,
		plans:    plans,
		handlers: handlers,
	}
}

func (r *PlanRepository) ActiveTradeIDs(ctx context.Context) ([]int, error) {
	if r.store == nil {
		return nil, nil
	}
	return r.store.ActiveTradeIDs(ctx)
}

func (r *PlanRepository) ListStrategyInstances(ctx context.Context, tradeID int) ([]database.StrategyInstanceRecord, error) {
	if r.store == nil {
		return nil, nil
	}
	return r.store.ListStrategyInstances(ctx, tradeID)
}

func (r *PlanRepository) BuildWatchers(recs []database.StrategyInstanceRecord) []*planWatcher {
	if len(recs) == 0 {
		return nil
	}
	groups := groupByPlanID(recs)
	watchers := make([]*planWatcher, 0, len(groups))
	for planID, list := range groups {
		handler := r.LookupHandler(planID)
		if handler == nil {
			continue
		}
		if watcher := r.BuildWatcher(planID, handler, list); watcher != nil {
			watchers = append(watchers, watcher)
		}
	}
	return watchers
}

func (r *PlanRepository) BuildWatcher(planID string, handler exit.PlanHandler, recs []database.StrategyInstanceRecord) *planWatcher {
	var root *exit.PlanInstance
	components := make(map[string]*exit.PlanInstance)
	for _, rec := range recs {
		inst := &exit.PlanInstance{
			Record: rec,
			Plan:   decodeJSONMap(rec.ParamsJSON),
			State:  decodeJSONMap(rec.StateJSON),
		}
		if strings.TrimSpace(rec.PlanComponent) == "" {
			root = inst
		} else {
			components[rec.PlanComponent] = inst
		}
	}
	if root == nil {
		return nil
	}

	planState, err := exit.DecodeTierPlanState(root.Record.StateJSON)
	if err != nil {
		logger.Warnf("PlanRepository: 解析 plan root 失败 trade=%d plan=%s err=%v", root.Record.TradeID, planID, err)
		return nil
	}
	symbol := strings.ToUpper(strings.TrimSpace(planState.Symbol))
	if symbol == "" {
		return nil
	}
	return &planWatcher{
		tradeID:    root.Record.TradeID,
		planID:     planID,
		symbol:     symbol,
		side:       strings.ToLower(strings.TrimSpace(planState.Side)),
		handler:    handler,
		rootInst:   root,
		components: components,
	}
}

func (r *PlanRepository) LookupHandler(planID string) exit.PlanHandler {
	planID = strings.TrimSpace(planID)
	if planID == "" || r.plans == nil || r.handlers == nil {
		return nil
	}
	tpl, ok := r.plans.Template(planID)
	if !ok {
		return nil
	}
	if handlerID := strings.TrimSpace(tpl.Handler); handlerID != "" {
		h, ok := r.handlers.Handler(handlerID)
		if ok {
			return h
		}
	}
	return nil
}

func (r *PlanRepository) PersistPlanState(ctx context.Context, inst *exit.PlanInstance, stateJSON string, status database.StrategyStatus) bool {
	if inst == nil {
		return false
	}
	stateJSON = strings.TrimSpace(stateJSON)
	if stateJSON == "" {
		stateJSON = "{}"
	}
	if r.store == nil {
		return false
	}
	if err := r.store.UpdateStrategyInstanceState(ctx, inst.Record.TradeID, inst.Record.PlanID, inst.Record.PlanComponent, stateJSON, status); err != nil {
		logger.Warnf("PlanRepository: 更新策略状态失败 trade=%d plan=%s err=%v", inst.Record.TradeID, inst.Record.PlanID, err)
		return false
	}
	inst.Record.StateJSON = stateJSON
	inst.Record.Status = status
	return true
}

func (r *PlanRepository) LogStateChange(ctx context.Context, inst *exit.PlanInstance, oldState string, oldStatus database.StrategyStatus, trigger string, source string, changeDetails map[string]any) {
	if r == nil || r.store == nil || inst == nil {
		return
	}
	if trigger == exit.PlanEventTypeTierHit {

		return
	}
	newState := normalizeStateJSON(inst.Record.StateJSON)
	oldState = normalizeStateJSON(oldState)
	statusChanged := oldStatus != inst.Record.Status
	stateChanged := strings.TrimSpace(newState) != strings.TrimSpace(oldState)
	if !stateChanged && !statusChanged {
		return
	}
	reason, _ := buildPlanChangeReason(inst, oldState, newState, oldStatus, inst.Record.Status, changeDetails)
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return
	}
	rec := database.StrategyChangeLogRecord{
		TradeID:         inst.Record.TradeID,
		InstanceID:      inst.Record.ID,
		PlanID:          inst.Record.PlanID,
		PlanComponent:   inst.Record.PlanComponent,
		ChangedField:    "plan_state",
		OldValue:        oldState,
		NewValue:        newState,
		TriggerSource:   pickTriggerSource(trigger, source),
		Reason:          reason,
		DecisionTraceID: inst.Record.DecisionTraceID,
	}
	if err := r.store.InsertStrategyChangeLog(ctx, rec); err != nil {
		logger.Warnf("PlanRepository: 写 strategy_change_log 失败 trade=%d plan=%s err=%v", inst.Record.TradeID, inst.Record.PlanID, err)
	}
}

func (r *PlanRepository) LogTradeOperation(ctx context.Context, inst *exit.PlanInstance, evt *exit.PlanEvent) {
	if r == nil || r.store == nil || inst == nil || evt == nil {
		return
	}
	op := operationFromEvent(evt.Type)
	if op == 0 {
		return
	}
	details := map[string]any{
		"plan_id":    inst.Record.PlanID,
		"component":  inst.Record.PlanComponent,
		"event_type": evt.Type,
	}
	if len(evt.Details) > 0 {
		details["context"] = evt.Details
	}
	appender, ok := r.store.(tradeOperationStore)
	if !ok {
		return
	}
	rec := database.TradeOperationRecord{
		FreqtradeID: inst.Record.TradeID,
		Symbol:      extractEventSymbol(inst, evt),
		Operation:   op,
		Details:     details,
		Timestamp:   time.Now(),
	}
	if err := appender.AppendTradeOperation(ctx, rec); err != nil {
		logger.Warnf("PlanRepository: 写 trade_operation_log 失败 trade=%d plan=%s err=%v", inst.Record.TradeID, inst.Record.PlanID, err)
	}
}

func groupByPlanID(recs []database.StrategyInstanceRecord) map[string][]database.StrategyInstanceRecord {
	out := make(map[string][]database.StrategyInstanceRecord)
	for _, rec := range recs {
		id := strings.TrimSpace(rec.PlanID)
		if id == "" {
			continue
		}
		out[id] = append(out[id], rec)
	}
	return out
}

func decodeJSONMap(raw string) map[string]any {
	if strings.TrimSpace(raw) == "" {
		return map[string]any{}
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return map[string]any{}
	}
	return data
}

func pickTriggerSource(trigger, source string) string {
	src := strings.TrimSpace(source)
	if src != "" {
		return src
	}
	return trigger
}

func buildPlanChangeReason(inst *exit.PlanInstance, oldState, newState string, oldStatus, newStatus database.StrategyStatus, changes map[string]any) (string, map[string]any) {
	lines, mergedChanges := collectStateChangeLines(inst, oldState, newState, changes)
	if oldStatus != newStatus {
		lines = append(lines, describeStatusChange(oldStatus, newStatus))
		if mergedChanges == nil {
			mergedChanges = make(map[string]any)
		}
		mergedChanges["status"] = map[string]any{
			"from": exit.StatusLabel(oldStatus),
			"to":   exit.StatusLabel(newStatus),
		}
	}
	reason := strings.TrimSpace(strings.Join(lines, "\n"))
	if len(mergedChanges) > 0 {
		if raw, err := json.Marshal(mergedChanges); err == nil {
			return fmt.Sprintf("%s\n变化详情: %s", reason, string(raw)), mergedChanges
		}
	}
	return reason, mergedChanges
}

func collectStateChangeLines(inst *exit.PlanInstance, oldState, newState string, changes map[string]any) ([]string, map[string]any) {
	component := strings.TrimSpace(inst.Record.PlanComponent)
	if len(changes) == 0 {

		return nil, nil
	}
	oldValues := make(map[string]string, len(changes))
	keys := make([]string, 0, len(changes))
	for key := range changes {
		oldValues[key] = extractStateValue(component, key, oldState)
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var parts []string
	filteredKeys := make([]string, 0, len(keys))
	filteredChanges := make(map[string]any, len(changes))
	for _, key := range keys {
		newVal := formatChangeValue(changes[key])
		oldVal := oldValues[key]
		if strings.TrimSpace(oldVal) == strings.TrimSpace(newVal) {
			continue
		}
		filteredKeys = append(filteredKeys, key)
		filteredChanges[key] = changes[key]
		parts = append(parts, describeFieldChange(component, key, oldVal, newVal))
	}
	if len(parts) == 0 {
		return nil, nil
	}
	return parts, buildMergedChanges(component, filteredKeys, filteredChanges, oldValues)
}

func buildMergedChanges(component string, keys []string, changes map[string]any, oldValues map[string]string) map[string]any {
	if len(keys) == 0 {
		return nil
	}
	out := make(map[string]any, len(keys))
	for _, key := range keys {
		entry := map[string]string{
			"from": oldValues[key],
			"to":   formatChangeValue(changes[key]),
		}
		if component != "" {
			entry["component"] = componentDisplayName(component) + "." + key
		} else {
			entry["component"] = key
		}
		out[key] = entry
	}
	return out
}

func extractStateValue(component, key, raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	if strings.TrimSpace(component) == "" {
		state, err := exit.DecodeTierPlanState(raw)
		if err != nil {
			return ""
		}
		switch key {
		case "stop_loss":
			return utils.FormatFloat(state.StopLossPrice)
		case "final_take_profit":
			return formatRelativePrice(state.EntryPrice, state.FinalTakeProfit, state.Side)
		case "final_stop_loss":
			return formatRelativePrice(state.EntryPrice, state.FinalStopLoss, state.Side)
		}
		return ""
	}
	state, err := exit.DecodeTierComponentState(raw)
	if err != nil {
		return ""
	}
	switch key {
	case "target_price":
		return utils.FormatFloat(state.TargetPrice)
	case "ratio":
		return utils.FormatFloat(state.Ratio)
	}
	return ""
}

func formatRelativePrice(entry, pct float64, side string) string {
	if entry <= 0 || pct == 0 {
		return ""
	}
	side = strings.ToLower(strings.TrimSpace(side))
	price := entry * (1 + pct)
	if side == "short" {
		price = entry * (1 - pct)
	}
	return utils.FormatFloat(price)
}

func describeFieldChange(component, key, oldVal, newVal string) string {
	label := fieldLabel(component, key)
	if strings.TrimSpace(oldVal) == "" {
		return fmt.Sprintf("设置%s为 %s", label, newVal)
	}
	return fmt.Sprintf("修改%s %s -> %s", label, oldVal, newVal)
}

func formatChangeValue(val interface{}) string {
	switch v := val.(type) {
	case float64:
		return utils.FormatFloat(v)
	case float32:
		return utils.FormatFloat(float64(v))
	case int:
		return fmt.Sprintf("%d", v)
	case int64:
		return fmt.Sprintf("%d", v)
	case string:
		return strings.TrimSpace(v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func fieldLabel(component, key string) string {
	if strings.TrimSpace(component) == "" {
		switch key {
		case "stop_loss":
			return "止损价"
		case "final_take_profit":
			return "最终止盈价"
		case "final_stop_loss":
			return "最终止损价"
		case "take_profit":
			return "止盈价"
		default:
			return key
		}
	}
	base := componentDisplayName(component)
	if root, tier := componentRole(component); root != "" {
		base = root
		if tier != "" {
			base = fmt.Sprintf("%s·%s", root, tier)
		}
	}
	switch key {
	case "ratio":
		return base + "比例"
	case "target_price":
		return base + "价格"
	case "status":
		return base + "状态"
	default:
		return fmt.Sprintf("%s%s", base, key)
	}
}

func componentDisplayName(component string) string {
	component = strings.TrimSpace(component)
	if strings.HasPrefix(strings.ToLower(component), "tier") {
		if idx, err := strconv.Atoi(strings.TrimPrefix(strings.ToLower(component), "tier")); err == nil {
			return ordinalTierName(idx)
		}
	}
	return component
}

// componentRole returns human-friendly root ("止盈"/"止损") and tier label ("第一目标"...) if present.
func componentRole(component string) (string, string) {
	comp := strings.ToLower(strings.TrimSpace(component))
	if comp == "" {
		return "", ""
	}
	parts := strings.Split(comp, ".")
	root := parts[0]
	var tierLabel string
	if len(parts) > 1 {
		if strings.HasPrefix(parts[1], "tier") {
			if idx, err := strconv.Atoi(strings.TrimPrefix(parts[1], "tier")); err == nil {
				tierLabel = ordinalTierName(idx)
			}
		}
	}
	switch {
	case strings.HasPrefix(root, "tp"):
		return "止盈", tierLabel
	case strings.HasPrefix(root, "sl"):
		return "止损", tierLabel
	default:
		return strings.ToUpper(root), tierLabel
	}
}

func ordinalTierName(idx int) string {
	switch idx {
	case 1:
		return "第一目标"
	case 2:
		return "第二目标"
	case 3:
		return "第三目标"
	case 4:
		return "第四目标"
	default:
		return fmt.Sprintf("第%d目标", idx)
	}
}

func describeStatusChange(oldStatus, newStatus database.StrategyStatus) string {
	return fmt.Sprintf("修改状态 %s -> %s", exit.StatusLabel(oldStatus), exit.StatusLabel(newStatus))
}

func normalizeStateJSON(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "{}"
	}
	return raw
}

func extractEventSymbol(inst *exit.PlanInstance, evt *exit.PlanEvent) string {
	if evt != nil && evt.Details != nil {
		if sym, ok := evt.Details["symbol"].(string); ok {
			return strings.ToUpper(strings.TrimSpace(sym))
		}
	}
	stateJSON := strings.TrimSpace(inst.Record.StateJSON)
	if stateJSON == "" {
		return ""
	}
	if strings.TrimSpace(inst.Record.PlanComponent) == "" {
		if state, err := exit.DecodeTierPlanState(stateJSON); err == nil {
			return strings.ToUpper(strings.TrimSpace(state.Symbol))
		}
	} else {
		if state, err := exit.DecodeTierComponentState(stateJSON); err == nil {
			return strings.ToUpper(strings.TrimSpace(state.Symbol))
		}
	}
	return ""
}

func operationFromEvent(evtType string) database.OperationType {
	switch evtType {
	case exit.PlanEventTypeTierHit, exit.PlanEventTypeTakeProfit, exit.PlanEventTypeFinalTakeProfit:
		return database.OperationTakeProfit
	case exit.PlanEventTypeStopLoss:
		return database.OperationStopLoss
	case exit.PlanEventTypeFinalStopLoss:
		return database.OperationFinalStop
	default:
		return 0
	}
}

func buildPlanSnapshots(recs []database.StrategyInstanceRecord) []exit.StrategyPlanSnapshot {
	if len(recs) == 0 {
		return nil
	}
	out := make([]exit.StrategyPlanSnapshot, 0, len(recs))
	for _, rec := range recs {
		out = append(out, exit.StrategyPlanSnapshot{
			PlanID:          rec.PlanID,
			PlanComponent:   rec.PlanComponent,
			PlanVersion:     rec.PlanVersion,
			StatusLabel:     exit.StatusLabel(rec.Status),
			StatusCode:      int(rec.Status),
			ParamsJSON:      rec.ParamsJSON,
			StateJSON:       rec.StateJSON,
			DecisionTraceID: rec.DecisionTraceID,
			CreatedAt:       rec.CreatedAt.Unix(),
			UpdatedAt:       rec.UpdatedAt.Unix(),
		})
	}
	return out
}
