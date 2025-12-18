package freqtrade

import (
	"context"
	"math"
	"sort"
	"strings"

	"brale/internal/gateway/database"
	"brale/internal/gateway/exchange"
	"brale/internal/strategy/exit"
)

type derivedExitPrices struct {
	StopLoss   float64
	TakeProfit float64
}

func deriveExitPricesFromStrategyInstances(recs []database.StrategyInstanceRecord, side string, currentPrice float64) derivedExitPrices {
	side = strings.ToLower(strings.TrimSpace(side))
	entry := 0.0
	rootSL := 0.0
	rootTP := 0.0

	var slTargets []float64
	var tpTargets []float64

	for _, rec := range recs {
		component := strings.TrimSpace(rec.PlanComponent)
		if component == "" {
			state, err := exit.DecodeTierPlanState(rec.StateJSON)
			if err != nil {
				continue
			}
			if entry <= 0 && state.EntryPrice > 0 {
				entry = state.EntryPrice
			}
			if side == "" && strings.TrimSpace(state.Side) != "" {
				side = strings.ToLower(strings.TrimSpace(state.Side))
			}
			if rootSL <= 0 {
				switch {
				case state.FinalStopLoss > 0:
					rootSL = state.FinalStopLoss
				case state.StopLossPrice > 0:
					rootSL = state.StopLossPrice
				case state.TrailingStopPrice > 0:
					rootSL = state.TrailingStopPrice
				}
			}
			if rootTP <= 0 {
				switch {
				case state.FinalTakeProfit > 0:
					rootTP = state.FinalTakeProfit
				case state.TakeProfitPrice > 0:
					rootTP = state.TakeProfitPrice
				}
			}
			continue
		}

		state, err := exit.DecodeTierComponentState(rec.StateJSON)
		if err != nil {
			continue
		}
		if entry <= 0 && state.EntryPrice > 0 {
			entry = state.EntryPrice
		}
		if side == "" && strings.TrimSpace(state.Side) != "" {
			side = strings.ToLower(strings.TrimSpace(state.Side))
		}
		if state.TargetPrice <= 0 {
			continue
		}

		status := strings.ToLower(strings.TrimSpace(state.Status))
		if status == "done" || status == "triggered" {
			continue
		}

		mode := strings.ToLower(strings.TrimSpace(state.Mode))
		if mode == "" {
			lc := strings.ToLower(component)
			switch {
			case strings.Contains(lc, "sl") || strings.Contains(lc, "stop"):
				mode = "stop_loss"
			case strings.Contains(lc, "tp") || strings.Contains(lc, "take"):
				mode = "take_profit"
			}
		}
		switch mode {
		case "stop_loss":
			slTargets = append(slTargets, state.TargetPrice)
		case "take_profit":
			tpTargets = append(tpTargets, state.TargetPrice)
		}
	}

	ref := currentPrice
	if ref <= 0 {
		ref = entry
	}

	out := derivedExitPrices{StopLoss: rootSL, TakeProfit: rootTP}
	if out.StopLoss <= 0 && len(slTargets) > 0 {
		out.StopLoss = pickNextStopLoss(slTargets, side, ref)
	}
	if out.TakeProfit <= 0 && len(tpTargets) > 0 {
		out.TakeProfit = pickNextTakeProfit(tpTargets, side, ref)
	}
	return out
}

func (m *Manager) hydrateAPIPositionExits(ctx context.Context, positions []exchange.APIPosition) {
	if m == nil || m.posRepo == nil || len(positions) == 0 {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	tradeIDs := make([]int, 0, len(positions))
	for _, pos := range positions {
		if pos.TradeID > 0 {
			tradeIDs = append(tradeIDs, pos.TradeID)
		}
	}
	recsByTrade, err := m.posRepo.ListStrategyInstancesByTradeIDs(ctx, tradeIDs)
	if err != nil || len(recsByTrade) == 0 {
		return
	}
	for i := range positions {
		pos := &positions[i]
		if pos.TradeID <= 0 {
			continue
		}
		recs := recsByTrade[pos.TradeID]
		if len(recs) == 0 {
			continue
		}
		derived := deriveExitPricesFromStrategyInstances(recs, pos.Side, pos.CurrentPrice)
		if pos.StopLoss <= 0 && derived.StopLoss > 0 {
			pos.StopLoss = derived.StopLoss
		}
		if pos.TakeProfit <= 0 && derived.TakeProfit > 0 {
			pos.TakeProfit = derived.TakeProfit
		}
	}
}

func (m *Manager) hydrateAPIPositionExit(ctx context.Context, pos *exchange.APIPosition) {
	if m == nil || m.posRepo == nil || pos == nil || pos.TradeID <= 0 {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	recs, err := m.posRepo.ListStrategyInstances(ctx, pos.TradeID)
	if err != nil || len(recs) == 0 {
		return
	}
	derived := deriveExitPricesFromStrategyInstances(recs, pos.Side, pos.CurrentPrice)
	if pos.StopLoss <= 0 && derived.StopLoss > 0 {
		pos.StopLoss = derived.StopLoss
	}
	if pos.TakeProfit <= 0 && derived.TakeProfit > 0 {
		pos.TakeProfit = derived.TakeProfit
	}
}

func pickNextStopLoss(prices []float64, side string, ref float64) float64 {
	list := positiveSorted(prices)
	if len(list) == 0 {
		return 0
	}
	switch strings.ToLower(strings.TrimSpace(side)) {
	case "long":
		best := 0.0
		for _, p := range list {
			if ref > 0 && p <= ref && p > best {
				best = p
			}
		}
		if best > 0 {
			return best
		}
		return list[0]
	case "short":
		best := 0.0
		for _, p := range list {
			if ref > 0 && p >= ref && (best == 0 || p < best) {
				best = p
			}
		}
		if best > 0 {
			return best
		}
		return list[len(list)-1]
	default:
		return pickNearest(list, ref)
	}
}

func pickNextTakeProfit(prices []float64, side string, ref float64) float64 {
	list := positiveSorted(prices)
	if len(list) == 0 {
		return 0
	}
	switch strings.ToLower(strings.TrimSpace(side)) {
	case "long":
		best := 0.0
		for _, p := range list {
			if ref > 0 && p >= ref && (best == 0 || p < best) {
				best = p
			}
		}
		if best > 0 {
			return best
		}
		return list[len(list)-1]
	case "short":
		best := 0.0
		for _, p := range list {
			if ref > 0 && p <= ref && p > best {
				best = p
			}
		}
		if best > 0 {
			return best
		}
		return list[0]
	default:
		return pickNearest(list, ref)
	}
}

func positiveSorted(prices []float64) []float64 {
	out := make([]float64, 0, len(prices))
	for _, p := range prices {
		if p > 0 {
			out = append(out, p)
		}
	}
	sort.Float64s(out)
	return out
}

func pickNearest(sorted []float64, ref float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if ref <= 0 {
		return sorted[0]
	}
	best := sorted[0]
	bestDist := math.Abs(sorted[0] - ref)
	for _, p := range sorted[1:] {
		d := math.Abs(p - ref)
		if d < bestDist {
			best = p
			bestDist = d
		}
	}
	return best
}
