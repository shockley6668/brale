package prompt

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

type ExitPlanPrompt struct {
	Key         string   `json:"key"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	JSONExample string   `json:"json_example"`
	Constraints []string `json:"constraints"`
}

type comboComponent struct {
	Key         string
	Alias       string
	Handler     string
	Mode        string
	Stage       string
	Kind        string
	DisplayName string
	Description string
	Constraints []string
}

type comboSpec struct {
	Key        string
	Title      string
	Components []comboComponent
}

func componentIndex() map[string]comboComponent {
	comps := make(map[string]comboComponent)
	for _, c := range listComponents() {
		comps[strings.ToLower(strings.TrimSpace(c.Key))] = c
	}
	return comps
}

var (
	comboPromptOnce sync.Once
	comboPromptMap  map[string]ExitPlanPrompt
)

func GenerateExitPlanPrompts() []ExitPlanPrompt {
	index := exitPlanPromptIndex()
	list := make([]ExitPlanPrompt, 0, len(index))
	for _, prompt := range index {
		list = append(list, prompt)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Key < list[j].Key })
	return list
}

func ExitPlanPrompts() map[string]ExitPlanPrompt {
	index := exitPlanPromptIndex()
	if len(index) == 0 {
		return nil
	}
	out := make(map[string]ExitPlanPrompt, len(index))
	for k, v := range index {
		out[k] = v
	}
	return out
}

func BuildPromptsFromCombos(comboPlan map[string]string) map[string]ExitPlanPrompt {
	if len(comboPlan) == 0 {
		return nil
	}
	compIdx := componentIndex()
	result := make(map[string]ExitPlanPrompt, len(comboPlan))
	for rawKey, planID := range comboPlan {
		norm := NormalizeComboKey(rawKey)
		if norm == "" {
			continue
		}
		if _, ok := result[norm]; ok {
			continue
		}
		spec, ok := comboSpecFromKey(norm, compIdx)
		if !ok {
			continue
		}
		prompt := buildComboPrompt(spec, planID)
		result[norm] = prompt
	}
	return result
}

func ExitPlanPromptByKey(key string) (ExitPlanPrompt, bool) {
	index := exitPlanPromptIndex()
	p, ok := index[NormalizeComboKey(key)]
	return p, ok
}

func NormalizeComboKey(key string) string {
	return strings.ToLower(strings.TrimSpace(key))
}

func exitPlanPromptIndex() map[string]ExitPlanPrompt {
	comboPromptOnce.Do(func() {
		combos := enumerateCombos()
		comboPromptMap = make(map[string]ExitPlanPrompt, len(combos))
		for _, combo := range combos {
			prompt := buildComboPrompt(combo, "plan_combo_main")
			comboPromptMap[NormalizeComboKey(prompt.Key)] = prompt
		}
	})
	return comboPromptMap
}

func buildComboPrompt(combo comboSpec, planID string) ExitPlanPrompt {
	if strings.TrimSpace(planID) == "" {
		planID = "plan_combo_main"
	}
	spec := map[string]any{
		"id": strings.TrimSpace(planID),
		"params": map[string]any{
			"children": buildChildren(combo.Components),
		},
	}
	data, _ := json.MarshalIndent(spec, "", "  ")

	constraints := aggregateConstraints(combo.Components)
	constraints = append([]string{
		`exit_plan.id 必须填 "plan_combo_main"，不得为空或替换其它 ID`,
		fmt.Sprintf("children 必须严格按组合 %s 生成，组件/handler 不可增删或替换", combo.Key),
	}, constraints...)
	return ExitPlanPrompt{
		Key:         combo.Key,
		Title:       combo.Title,
		Description: describeCombo(combo.Components),
		JSONExample: string(data),
		Constraints: constraints,
	}
}

func enumerateCombos() []comboSpec {
	takeProfit := listTakeProfitComponents()

	stopLoss := listStopLossComponents()

	var combos []comboSpec
	for _, tp := range takeProfit {
		for _, sl := range stopLoss {
			key := fmt.Sprintf("%s__%s", tp.Key, sl.Key)
			title := fmt.Sprintf("%s + %s", tp.DisplayName, sl.DisplayName)
			combos = append(combos, comboSpec{
				Key:        key,
				Title:      title,
				Components: []comboComponent{tp, sl},
			})
		}
	}

	for _, sl := range stopLoss {
		key := fmt.Sprintf("tp_tiers__tp_atr__%s", sl.Key)
		title := fmt.Sprintf("多阶段止盈 + ATR 锁盈 + %s", sl.DisplayName)
		combos = append(combos, comboSpec{
			Key:        key,
			Title:      title,
			Components: []comboComponent{takeProfit[1], takeProfit[2], sl},
		})
	}
	return combos
}

func comboSpecFromKey(key string, index map[string]comboComponent) (comboSpec, bool) {
	parts := strings.Split(key, "__")
	if len(parts) == 0 {
		return comboSpec{}, false
	}
	var comps []comboComponent
	for _, part := range parts {
		id := NormalizeComboKey(part)
		comp, ok := index[id]
		if !ok {
			return comboSpec{}, false
		}
		comps = append(comps, comp)
	}
	if len(comps) == 0 {
		return comboSpec{}, false
	}
	return comboSpec{
		Key:        key,
		Title:      describeCombo(comps),
		Components: comps,
	}, true
}

func listComponents() []comboComponent {
	var res []comboComponent
	res = append(res, listTakeProfitComponents()...)
	res = append(res, listStopLossComponents()...)
	return res
}

func listTakeProfitComponents() []comboComponent {
	return []comboComponent{
		{
			Key:         "tp_single",
			Alias:       "tp_single",
			Handler:     "tier_take_profit",
			Stage:       "single",
			Kind:        "tp",
			DisplayName: "单一止盈",
			Description: "一个止盈目标，比例 100%",
			Constraints: []string{
				"target_price 必须高于开仓价（多头）或低于开仓价（空头）",
				"ratio 固定为 1.0",
			},
		},
		{
			Key:         "tp_tiers",
			Alias:       "tp_tiers",
			Handler:     "tier_take_profit",
			Stage:       "tiers",
			Kind:        "tp",
			DisplayName: "多阶段止盈",
			Description: "分段止盈，价格按多空方向单调（多头递增/空头递减），比例合计 100%",
			Constraints: []string{
				"多头：target_price 需严格递增且高于开仓价；空头：需严格递减且低于开仓价",
				"各段 ratio 需 >0 且总和为 1",
				"tiers 数量应该大于2",
			},
		},
		{
			Key:         "tp_atr",
			Alias:       "tp_atr",
			Handler:     "atr_trailing",
			Mode:        "take_profit",
			Stage:       "atr",
			Kind:        "tp",
			DisplayName: "ATR 锁盈",
			Description: "ATR 倍数触发追踪止盈",
			Constraints: []string{
				"atr_value、trigger_multiplier、trail_multiplier 均需 >0",
				"trigger_multiplier 必须大于 trail_multiplier",
			},
		},
	}
}

func listStopLossComponents() []comboComponent {
	return []comboComponent{
		{
			Key:         "sl_single",
			Alias:       "sl_single",
			Handler:     "tier_stop_loss",
			Stage:       "single",
			Kind:        "sl",
			DisplayName: "单一止损",
			Description: "一个固定止损价，比例 100%",
			Constraints: []string{
				"target_price 必须低于开仓价（多头）或高于开仓价（空头）",
				"ratio 固定为 1.0",
			},
		},
		{
			Key:         "sl_tiers",
			Alias:       "sl_tiers",
			Handler:     "tier_stop_loss",
			Stage:       "tiers",
			Kind:        "sl",
			DisplayName: "多阶段止损",
			Description: "分段止损，价格按多空方向单调（多头递减/空头递增），比例合计 100%",
			Constraints: []string{
				"多头：target_price 需严格递减且低于开仓价；空头：需严格递增且高于开仓价",
				"ratio 合计为 1，并与止盈段的剩余仓位匹配",
				"tiers 数量应该大于2",
			},
		},
		{
			Key:         "sl_atr",
			Alias:       "sl_atr",
			Handler:     "atr_trailing",
			Mode:        "stop_loss",
			Stage:       "atr",
			Kind:        "sl",
			DisplayName: "ATR 追踪止损",
			Description: "ATR 倍数用于动态抬升/下调止损",
			Constraints: []string{
				"atr_value、trigger_multiplier、trail_multiplier 均需 >0",
				"触发倍数需大于追踪倍数",
			},
		},
	}
}

func buildChildren(components []comboComponent) []map[string]any {
	children := make([]map[string]any, 0, len(components))
	for _, comp := range components {
		child := map[string]any{
			"component": comp.Alias,
			"handler":   comp.Handler,
			"params":    buildParams(comp),
		}
		children = append(children, child)
	}
	return children
}

func buildParams(comp comboComponent) map[string]any {
	prefix := strings.ToUpper(strings.TrimSpace(comp.Kind))
	if prefix == "" {
		prefix = strings.ToUpper(strings.TrimSpace(comp.Alias))
	}
	params := map[string]any{}

	if comp.Mode != "" && comp.Stage == "atr" {
		params["mode"] = comp.Mode
	}
	switch comp.Stage {
	case "single":
		params["tiers"] = []any{
			map[string]any{
				"target_price": placeholder(fmt.Sprintf("%s_SINGLE_PRICE", prefix)),
				"ratio":        1.0,
			},
		}
	case "tiers":
		params["tiers"] = []any{
			map[string]any{
				"target_price": placeholder(fmt.Sprintf("%s_TIER1_PRICE", prefix)),
				"ratio":        0.4,
			},
			map[string]any{
				"target_price": placeholder(fmt.Sprintf("%s_TIER2_PRICE", prefix)),
				"ratio":        0.35,
			},
			map[string]any{
				"target_price": placeholder(fmt.Sprintf("%s_TIER3_PRICE", prefix)),
				"ratio":        0.25,
			},
		}
	case "atr":
		params["atr_value"] = placeholder(fmt.Sprintf("%s_ATR_VALUE", prefix))
		params["trigger_multiplier"] = placeholder(fmt.Sprintf("%s_TRIGGER_MULTIPLIER", prefix))
		params["trail_multiplier"] = placeholder(fmt.Sprintf("%s_TRAIL_MULTIPLIER", prefix))
		params["initial_stop_multiplier"] = placeholder(fmt.Sprintf("%s_INITIAL_STOP_MULTIPLIER", prefix))
	}
	return params
}

func placeholder(tag string) string {
	tag = strings.ToUpper(strings.TrimSpace(tag))
	tag = strings.ReplaceAll(tag, ".", "_")
	return fmt.Sprintf("<%s>", tag)
}

func aggregateConstraints(components []comboComponent) []string {
	seen := make(map[string]struct{})
	list := []string{
		"action可选：hold / open_long / open_short / update_exit_plan",
		"当action为hold时，只需返回json中的 symbol,action,reasoning,请在reasoning说明理由",
		"其余的action都必须返回完整的json 数据，不可省略",
		"所有 target_price 字段必须使用绝对价格,tp代表take price,sl代表stop loss",
		"children 中每个节点必须同时包含 component / handler / params 且键名不可缺省，禁止省略 handler 或添加额外字段",
		"在update_exit_plan时，只可修改未触发阶段的策略，若已触发，则不可修改",
		"在action 为 open_long/open_short时，请按照当前趋势判断，为json填入正确的值",
	}
	for _, comp := range components {
		for _, line := range comp.Constraints {
			key := strings.TrimSpace(line)
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			list = append(list, line)
		}
	}
	return list
}

func describeCombo(components []comboComponent) string {
	parts := make([]string, 0, len(components))
	for _, comp := range components {
		parts = append(parts, comp.DisplayName)
	}
	return strings.Join(parts, " + ")
}

const decisionConstraintBase = `### 决策输出要求
- 仅输出 JSON 数组，每个元素代表一次操作，必须包含 symbol/action/reasoning/position_size_usd/leverage/confidence/exit_plan。
- action 为 open_long/open_short 时：字段不可缺省，止盈/止损仅通过 exit_plan 描述。
- action 为 update_exit_plan：必须附带完整 exit_plan（根节点 + 全部组件），且仅能修改状态为 waiting/pending 的段位。
- 无操作时输出 [{"symbol":"BTCUSDT","action":"hold","reasoning":"简明理由"}]。
`

const fullDecisionTemplate = `[
  {
    "symbol": "ETH/USDT",
    "action": "open_long",
    "position_size_usd": 500,
    "leverage": 3,
    "confidence": 70,
    "reasoning": "请在此说明触发理由，100字以内。",
    "exit_plan": %s
  }
]`

func DecisionConstraint(key string) (string, string, bool) {
	prompt, ok := ExitPlanPromptByKey(key)
	if !ok {
		return "", "", false
	}
	var builder strings.Builder
	builder.WriteString(decisionConstraintBase)
	builder.WriteString("\n### Exit Plan 约束\n")
	for _, line := range prompt.Constraints {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		builder.WriteString("- " + line + "\n")
	}
	if prompt.JSONExample != "" {
		builder.WriteString("\n### Exit Plan 示例 JSON\n")
		builder.WriteString(prompt.JSONExample)
		builder.WriteString("\n")
	}
	example := fmt.Sprintf(fullDecisionTemplate, prompt.JSONExample)
	return builder.String(), example, true
}
