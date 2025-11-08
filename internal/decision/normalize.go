package decision

import "strings"

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
	case "adjust_stop", "adjust_stop_loss", "move_stop", "move_stop_loss", "update_stop", "move_sl", "tighten_stop", "trail_stop":
		return "adjust_stop_loss"
	default:
		return a
	}
}
