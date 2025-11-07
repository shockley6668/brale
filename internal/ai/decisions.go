package ai

import "strings"

// normalizeAndAlignDecisions 会先对动作做归一化，再结合当前持仓调整 close_* 方向。
func normalizeAndAlignDecisions(ds []Decision, positions []PositionSnapshot) []Decision {
	if len(ds) == 0 {
		return ds
	}
	out := make([]Decision, len(ds))
	copy(out, ds)
	for i := range out {
		out[i].Action = NormalizeAction(out[i].Action)
	}
	return alignCloseActions(out, positions)
}

func alignCloseActions(ds []Decision, positions []PositionSnapshot) []Decision {
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
