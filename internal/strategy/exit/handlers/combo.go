package handlers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"brale/internal/gateway/database"
	"brale/internal/strategy/exit"
)

const (
	comboHandlerID        = "combo_group"
	metaHandlerKey        = "__combo_handler"
	metaOriginalCompKey   = "__combo_component"
	metaAliasKey          = "__combo_alias"
	metaChildParamsKey    = "__combo_params"
	metaChildNameKey      = "component"
	metaChildHandlerKey   = "handler"
	metaChildParamsField  = "params"
	defaultChildAliasPref = "comp"
)

type comboHandler struct {
	reg *exit.HandlerRegistry
}

func newComboHandler(reg *exit.HandlerRegistry) *comboHandler {
	return &comboHandler{reg: reg}
}

func (h *comboHandler) ID() string { return comboHandlerID }

func (h *comboHandler) Validate(params map[string]any) error {
	children := parseChildSpecs(params)
	if children.Len() == 0 {
		return fmt.Errorf("combo_group: 需提供 children 列表")
	}
	if children.Len() > 4 {
		return fmt.Errorf("combo_group: children 数量过多（最多 4 个）")
	}
	aliasSeen := make(map[string]bool, children.Len())
	for idx, child := range children.items {
		if strings.TrimSpace(child.Handler) == "" {
			return fmt.Errorf("combo_group: child #%d 缺少 handler", idx+1)
		}
		if h.reg == nil {
			return fmt.Errorf("combo_group: handler registry 未初始化")
		}
		if _, ok := h.reg.Handler(child.Handler); !ok {
			return fmt.Errorf("combo_group: handler 未注册 %s", child.Handler)
		}
		if len(child.Params) == 0 {
			return fmt.Errorf("combo_group: child #%d 缺少 params", idx+1)
		}
		if alias := strings.TrimSpace(child.Component); alias != "" {
			if aliasSeen[alias] {
				return fmt.Errorf("combo_group: component %s 重复", alias)
			}
			aliasSeen[alias] = true
		}
	}
	return nil
}

func (h *comboHandler) Instantiate(ctx context.Context, args exit.InstantiateArgs) ([]exit.PlanInstance, error) {
	if err := h.Validate(args.PlanSpec); err != nil {
		return nil, err
	}
	children := parseChildSpecs(args.PlanSpec)
	rootPlan := cloneMap(args.PlanSpec)
	rootPlan["children"] = children.raw
	now := time.Now()
	rootState := exit.TierPlanState{
		Symbol:        resolveSymbol(args),
		Side:          resolveSide(args),
		EntryPrice:    args.EntryPrice,
		LastUpdatedAt: now.Unix(),
	}
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
		Plan: rootPlan,
	}
	var insts []exit.PlanInstance
	insts = append(insts, root)
	for idx, spec := range children.items {
		handler, _ := h.reg.Handler(spec.Handler)
		if handler == nil {
			return nil, fmt.Errorf("combo_group: handler 未注册 %s", spec.Handler)
		}
		alias := spec.Component
		if alias == "" {
			alias = fmt.Sprintf("%s%d", defaultChildAliasPref, idx+1)
		}
		childArgs := exit.InstantiateArgs{
			TradeID:       args.TradeID,
			PlanID:        args.PlanID,
			PlanVersion:   args.PlanVersion,
			PlanSpec:      spec.Params,
			Decision:      args.Decision,
			EntryPrice:    args.EntryPrice,
			Side:          args.Side,
			Symbol:        args.Symbol,
			DecisionTrace: args.DecisionTrace,
		}
		childInsts, err := handler.Instantiate(ctx, childArgs)
		if err != nil {
			return nil, fmt.Errorf("combo_group: 子计划 %s 初始化失败: %w", alias, err)
		}
		for _, inst := range childInsts {
			copy := inst
			origComp := copy.Record.PlanComponent
			copy.Record.PlanID = args.PlanID
			copy.Record.PlanComponent = combineComponent(alias, origComp)
			copy.Plan = wrapChildParams(spec.Handler, alias, origComp, inst.Plan)
			copy.Record.ParamsJSON = database.EncodeParams(copy.Plan)
			copy.Record.PlanVersion = normalizePlanVersion(args.PlanVersion)
			copy.Record.TradeID = args.TradeID
			copy.Record.DecisionTraceID = strings.TrimSpace(args.DecisionTrace)
			insts = append(insts, copy)
		}
	}
	return insts, nil
}

func (h *comboHandler) OnPrice(ctx context.Context, inst exit.PlanInstance, price float64) (*exit.PlanEvent, error) {
	meta, childInst, handler, err := h.childInstance(inst)
	if err != nil {
		return nil, err
	}
	if handler == nil {
		return nil, nil
	}
	evt, err := handler.OnPrice(ctx, childInst, price)
	if err != nil || evt == nil {
		return evt, err
	}
	return wrapChildEvent(evt, meta.alias, inst.Record.PlanComponent), nil
}

func (h *comboHandler) OnAdjust(ctx context.Context, inst exit.PlanInstance, params map[string]any) (*exit.PlanEvent, error) {
	meta, childInst, handler, err := h.childInstance(inst)
	if err != nil {
		return nil, err
	}
	if handler == nil {
		return nil, nil
	}
	evt, err := handler.OnAdjust(ctx, childInst, params)
	if err != nil || evt == nil {
		return evt, err
	}
	return wrapChildEvent(evt, meta.alias, inst.Record.PlanComponent), nil
}

type childSpec struct {
	Component string
	Handler   string
	Params    map[string]any
}

type childSpecCollection struct {
	items []childSpec
	raw   []any
}

func (c childSpecCollection) Len() int { return len(c.items) }

func parseChildSpecs(params map[string]any) childSpecCollection {
	if params == nil {
		return childSpecCollection{}
	}
	rawList, _ := params["children"].([]any)
	items := make([]childSpec, 0, len(rawList))
	rawAny := make([]any, 0, len(rawList))
	for _, raw := range rawList {
		cmap, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		rawAny = append(rawAny, cmap)
		spec := childSpec{
			Component: strings.TrimSpace(asString(cmap[metaChildNameKey])),
			Handler:   strings.TrimSpace(asString(cmap[metaChildHandlerKey])),
		}
		if ps, ok := cmap[metaChildParamsField].(map[string]any); ok {
			spec.Params = ps
		} else {
			spec.Params = map[string]any{}
		}
		items = append(items, spec)
	}
	return childSpecCollection{items: items, raw: rawAny}
}

func asString(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

type comboMetadata struct {
	handler       string
	originalComp  string
	alias         string
	childPlan     map[string]any
	compositeComp string
}

func wrapChildParams(handlerID, alias, original string, childPlan map[string]any) map[string]any {
	clone := map[string]any{}
	for k, v := range childPlan {
		clone[k] = v
	}
	snapshot := cloneMap(childPlan)
	clone[metaHandlerKey] = handlerID
	clone[metaOriginalCompKey] = original
	clone[metaAliasKey] = alias
	clone[metaChildParamsKey] = snapshot
	return clone
}

func (h *comboHandler) childInstance(inst exit.PlanInstance) (comboMetadata, exit.PlanInstance, exit.PlanHandler, error) {
	meta := comboMetadata{
		compositeComp: inst.Record.PlanComponent,
	}
	handlerID := strings.TrimSpace(asString(inst.Plan[metaHandlerKey]))
	if handlerID == "" {
		return meta, inst, nil, nil
	}
	handler, ok := h.reg.Handler(handlerID)
	if !ok || handler == nil {
		return meta, inst, nil, fmt.Errorf("combo_group: 未知子 handler %s", handlerID)
	}
	meta.handler = handlerID
	meta.originalComp = strings.TrimSpace(asString(inst.Plan[metaOriginalCompKey]))
	meta.alias = strings.TrimSpace(asString(inst.Plan[metaAliasKey]))
	if params, ok := inst.Plan[metaChildParamsKey].(map[string]any); ok {
		meta.childPlan = params
	}
	child := inst
	if meta.childPlan != nil {
		child.Plan = meta.childPlan
	}
	child.Record.PlanComponent = meta.originalComp
	return meta, child, handler, nil
}

func wrapChildEvent(evt *exit.PlanEvent, alias, composite string) *exit.PlanEvent {
	out := *evt
	out.PlanComponent = composite
	if out.Details == nil {
		out.Details = map[string]any{}
	}
	out.Details["combo_alias"] = alias
	return &out
}

func combineComponent(alias, original string) string {
	alias = strings.TrimSpace(alias)
	if original == "" {
		return alias
	}
	if alias == "" {
		return original
	}
	return alias + "." + original
}

func resolveSymbol(args exit.InstantiateArgs) string {
	symbol := strings.ToUpper(strings.TrimSpace(args.Symbol))
	if symbol != "" {
		return symbol
	}
	if sym := strings.ToUpper(strings.TrimSpace(args.Decision.Symbol)); sym != "" {
		return sym
	}
	return ""
}

func resolveSide(args exit.InstantiateArgs) string {
	side := normalizeSide(args.Side)
	if side != "" {
		return side
	}
	action := strings.ToLower(strings.TrimSpace(args.Decision.Action))
	switch {
	case strings.Contains(action, "short"):
		return "short"
	case strings.Contains(action, "long"):
		return "long"
	default:
		return ""
	}
}
