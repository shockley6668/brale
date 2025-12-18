package decision

import "strings"

// NormalizeAndAlignDecisions 会先对动作做归一化，再结合当前持仓调整 close_* 方向。
func NormalizeAndAlignDecisions(ds []Decision, positions []PositionSnapshot) []Decision {
	if len(ds) == 0 {
		return ds
	}
	out := make([]Decision, len(ds))
	copy(out, ds)
	for i := range out {
		out[i].Action = NormalizeAction(out[i].Action)
	}
	return AlignCloseActions(out, positions)
}

func AlignCloseActions(ds []Decision, positions []PositionSnapshot) []Decision {
	if len(ds) == 0 || len(positions) == 0 {
		return ds
	}
	sideMap := make(map[string]string, len(positions))
	for _, pos := range positions {
		sym := strings.ToUpper(strings.TrimSpace(pos.Symbol))
		if sym == "" {
			continue
		}
		side := strings.ToLower(strings.TrimSpace(pos.Side))
		if side == "" {
			continue
		}
		if _, ok := sideMap[sym]; !ok {
			sideMap[sym] = side
		}
	}
	if len(sideMap) == 0 {
		return ds
	}
	for i := range ds {
		sym := strings.ToUpper(strings.TrimSpace(ds[i].Symbol))
		if sym == "" {
			continue
		}
		side := sideMap[sym]
		if side == "" {
			continue
		}
		switch ds[i].Action {
		case "close_long":
			if side == "short" {
				ds[i].Action = "close_short"
			}
		case "close_short":
			if side == "long" {
				ds[i].Action = "close_long"
			}
		}
	}
	return ds
}

// AttachDecisionProfiles 缺省情况下补齐 profile/context_tag，方便后续归档。
func AttachDecisionProfiles(ds []Decision, reports []FeatureReport) {
	if len(ds) == 0 || len(reports) == 0 {
		return
	}
	type meta struct {
		profile string
		context string
	}
	metaBySymbol := make(map[string]meta, len(reports))
	for _, rep := range reports {
		sym := strings.ToUpper(strings.TrimSpace(rep.Symbol))
		if sym == "" {
			continue
		}
		if _, ok := metaBySymbol[sym]; ok {
			continue
		}
		metaBySymbol[sym] = meta{
			profile: strings.TrimSpace(rep.Profile),
			context: strings.TrimSpace(rep.ContextTag),
		}
	}
	if len(metaBySymbol) == 0 {
		return
	}
	for i := range ds {
		sym := strings.ToUpper(strings.TrimSpace(ds[i].Symbol))
		if sym == "" {
			continue
		}
		info, ok := metaBySymbol[sym]
		if !ok {
			continue
		}
		if strings.TrimSpace(ds[i].Profile) == "" && info.profile != "" {
			ds[i].Profile = info.profile
		}
		if strings.TrimSpace(ds[i].ContextTag) == "" && info.context != "" {
			ds[i].ContextTag = info.context
		}
	}
}

// NormalizeAction 统一动作名称，兼容 buy/long 等同义词
func NormalizeAction(a string) string {
	replacer := strings.NewReplacer(" ", "_", "-", "_")
	a = strings.ToLower(strings.TrimSpace(a))
	a = replacer.Replace(a)
	switch a {
	case "wait", "stay", "neutral":
		return "hold"
	case "hold":
		return "hold"
	case "buy", "long", "open", "enter_long", "go_long", "open_long", "buy_long":
		return "open_long"
	case "sell", "short", "enter_short", "go_short", "open_short", "sell_short":
		return "open_short"
	case "close_long", "exit_long", "flat_long", "take_profit_long", "close", "exit", "flat", "close_position":
		return "close_long"
	case "close_short", "exit_short", "flat_short", "take_profit_short":
		return "close_short"
	case "partial_take_profit", "partial_close", "reduce", "scale_out":
		return "partial_close"
	case "adjust_stop", "adjust_stop_loss", "move_stop", "move_stop_loss", "update_stop", "move_sl", "tighten_stop", "trail_stop",
		"adjust_take_profit", "adjust_tp", "move_take_profit", "move_tp", "update_take_profit", "update_tp", "tighten_tp",
		"update_plan", "adjust_plan", "modify_plan", "update_exit_plan":
		return "update_exit_plan"
	default:
		return a
	}
}

// OrderAndDedup 决策排序与去重：
// - 优先级：close_* > open_* > hold/wait
// - 防重复：同一币种同方向的 open_* 仅保留第一条
func OrderAndDedup(ds []Decision) []Decision {
	if len(ds) <= 1 {
		return ds
	}
	pri := func(a string) int {
		switch a {
		case "close_long", "close_short":
			return 1
		case "update_exit_plan":
			return 2
		case "open_long", "open_short":
			return 3
		case "hold", "wait":
			return 4
		default:
			return 9
		}
	}
	out := make([]Decision, len(ds))
	copy(out, ds)
	for i := 0; i < len(out)-1; i++ {
		min := i
		for j := i + 1; j < len(out); j++ {
			if pri(out[j].Action) < pri(out[min].Action) {
				min = j
			}
		}
		if min != i {
			out[i], out[min] = out[min], out[i]
		}
	}
	seen := map[string]bool{}
	filtered := make([]Decision, 0, len(out))
	for _, d := range out {
		if d.Action == "open_long" || d.Action == "open_short" {
			key := d.Symbol + "#" + d.Action
			if seen[key] {
				continue
			}
			seen[key] = true
		}
		filtered = append(filtered, d)
	}
	return filtered
}
