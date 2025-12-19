package engine

import (
	"fmt"
	"strings"

	"brale/internal/agent/interfaces"
	"brale/internal/decision"
	"brale/internal/exitplan"
	"brale/internal/logger"
	"brale/internal/pkg/utils"
	"brale/internal/profile"
	"brale/internal/strategy/exit"
)

type ExitPlanPolicy struct {
	exitPlans    *exitplan.Registry
	planHandlers *exit.HandlerRegistry
	profileMgr   *profile.Manager
	mktService   interfaces.MarketService
}

func NewExitPlanPolicy(plans *exitplan.Registry, handlers *exit.HandlerRegistry, profiles *profile.Manager, mkt interfaces.MarketService) *ExitPlanPolicy {
	return &ExitPlanPolicy{
		exitPlans:    plans,
		planHandlers: handlers,
		profileMgr:   profiles,
		mktService:   mkt,
	}
}

func (p *ExitPlanPolicy) Apply(decisions []decision.Decision) []decision.Decision {
	if len(decisions) == 0 {
		return nil
	}
	filtered := decisions[:0]
	for _, d := range decisions {
		if d.ExitPlan != nil {
			p.injectExitPlanMetrics(&d)
		}
		if d.Action == "open_long" || d.Action == "open_short" {
			version, err := p.validateExitPlan(d.Symbol, d.ExitPlan)
			if err != nil {
				logger.Warnf("exit_plan 校验失败 symbol=%s err=%v", strings.ToUpper(strings.TrimSpace(d.Symbol)), err)
				continue
			}
			d.ExitPlanVersion = version
		}
		filtered = append(filtered, d)
	}
	return filtered
}

func (p *ExitPlanPolicy) injectExitPlanMetrics(dec *decision.Decision) {
	if dec == nil || dec.ExitPlan == nil {
		return
	}
	p.injectPlanSpec(dec.Symbol, dec.ExitPlan, strings.TrimSpace(dec.ExitPlan.ID))
}

func (p *ExitPlanPolicy) injectPlanSpec(symbol string, spec *decision.ExitPlanSpec, label string) {
	if spec == nil {
		return
	}
	handlerID := ""
	if p.exitPlans != nil {
		if tpl, ok := p.exitPlans.Template(spec.ID); ok {
			handlerID = strings.TrimSpace(tpl.Handler)
		}
	}
	if spec.Params == nil {
		spec.Params = make(map[string]any)
	}
	p.injectParamsByHandler(symbol, label, handlerID, spec.Params)
	for i := range spec.Components {
		childLabel := strings.TrimSpace(spec.Components[i].ID)
		if childLabel == "" {
			childLabel = label
		}
		p.injectPlanSpec(symbol, &spec.Components[i], childLabel)
	}
}

func (p *ExitPlanPolicy) injectParamsByHandler(symbol, label, handlerID string, params map[string]any) {
	handlerKey := strings.TrimSpace(handlerID)
	switch handlerKey {
	case "atr_trailing", "atr_trailing_takeprofit", "atr_trailing_stop":
		p.ensureATRValue(symbol, label, handlerKey, params)
	case "combo_group":
		p.injectComboChildren(symbol, label, params)
	default:
		p.injectComboChildren(symbol, label, params)
	}
}

func (p *ExitPlanPolicy) injectComboChildren(symbol, label string, params map[string]any) {
	if params == nil {
		return
	}
	rawChildren, ok := params["children"]
	if !ok {
		return
	}
	list, ok := rawChildren.([]any)
	if !ok || len(list) == 0 {
		return
	}
	for _, entry := range list {
		childMap, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		handler := strings.TrimSpace(utils.AsString(childMap["handler"]))
		if handler == "" {
			continue
		}
		component := strings.TrimSpace(utils.AsString(childMap["component"]))
		childLabel := label
		if component != "" {
			if childLabel != "" {
				childLabel = childLabel + "/" + component
			} else {
				childLabel = component
			}
		}
		childParams, _ := childMap["params"].(map[string]any)
		if childParams == nil {
			childParams = make(map[string]any)
			childMap["params"] = childParams
		}
		p.injectParamsByHandler(symbol, childLabel, handler, childParams)
	}
}

func (p *ExitPlanPolicy) ensureATRValue(symbol, label, handler string, params map[string]any) {
	if params == nil {
		return
	}
	if v, ok := utils.AsFloat(params["atr_value"]); ok && v > 0 {
		params["atr_value"] = v
		return
	}

	atr, ok := p.mktService.GetATR(symbol)

	if !ok || atr <= 0 {
		logger.Warnf("exit_plan: 无法注入 ATR 参数 symbol=%s target=%s handler=%s", strings.ToUpper(strings.TrimSpace(symbol)), label, handler)
		return
	}
	params["atr_value"] = atr
	logger.Infof("exit_plan: 自动注入 ATR 参数 symbol=%s target=%s handler=%s atr=%.4f", strings.ToUpper(strings.TrimSpace(symbol)), label, handler, atr)
}

func (p *ExitPlanPolicy) validateExitPlan(symbol string, spec *decision.ExitPlanSpec) (int, error) {
	if spec == nil || strings.TrimSpace(spec.ID) == "" {
		return 0, fmt.Errorf("缺少 exit_plan")
	}
	if p.profileMgr == nil {
		return 0, fmt.Errorf("profile manager 未初始化")
	}
	runtime, ok := p.profileMgr.Resolve(symbol)
	if !ok || runtime == nil {
		return 0, fmt.Errorf("未找到 %s 对应的 profile", strings.ToUpper(strings.TrimSpace(symbol)))
	}
	if !exitplan.PlanAllowed(spec.ID, runtime.Definition.ExitPlans.Allowed) {
		return 0, fmt.Errorf("plan %s not in profile %s whitelist", spec.ID, runtime.Definition.Name)
	}
	if p.exitPlans == nil {
		return 0, fmt.Errorf("exit plan registry 未初始化")
	}
	tpl, err := p.exitPlans.Validate(spec.ID, spec.Params)
	if err != nil {
		return 0, err
	}
	if handlerID := strings.TrimSpace(tpl.Handler); handlerID != "" && p.planHandlers != nil {
		if handler, ok := p.planHandlers.Handler(handlerID); ok {
			if err := handler.Validate(spec.Params); err != nil {
				return 0, fmt.Errorf("plan handler %s 校验失败: %w", handlerID, err)
			}
		}
	}
	for i := range spec.Components {
		child := spec.Components[i]
		if strings.TrimSpace(child.ID) == "" {
			continue
		}
		if _, err := p.exitPlans.Validate(child.ID, child.Params); err != nil {
			return 0, fmt.Errorf("子计划 %s 校验失败: %w", child.ID, err)
		}
	}
	return tpl.Version, nil
}
