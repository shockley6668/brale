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
	d := newExitDerivation(side)
	for _, rec := range recs {
		d.absorbStrategyInstance(rec)
	}
	return d.finalize(currentPrice)
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

type exitDerivation struct {
	side      string
	entry     float64
	rootSL    float64
	rootTP    float64
	slTargets []float64
	tpTargets []float64
}

func newExitDerivation(side string) *exitDerivation {
	return &exitDerivation{side: strings.ToLower(strings.TrimSpace(side))}
}

func (d *exitDerivation) absorbStrategyInstance(rec database.StrategyInstanceRecord) {
	component := strings.TrimSpace(rec.PlanComponent)
	if component == "" {
		state, err := exit.DecodeTierPlanState(rec.StateJSON)
		if err != nil {
			return
		}
		d.absorbTierPlanState(state)
		return
	}

	state, err := exit.DecodeTierComponentState(rec.StateJSON)
	if err != nil {
		return
	}
	d.absorbTierComponentState(component, state)
}

func (d *exitDerivation) absorbTierPlanState(state exit.TierPlanState) {
	d.maybeSetEntrySide(state.EntryPrice, state.Side)

	if d.rootSL <= 0 {
		d.rootSL = pickRootStopLoss(state)
	}
	if d.rootTP <= 0 {
		d.rootTP = pickRootTakeProfit(state)
	}
}

func (d *exitDerivation) absorbTierComponentState(component string, state exit.TierComponentState) {
	d.maybeSetEntrySide(state.EntryPrice, state.Side)
	if state.TargetPrice <= 0 {
		return
	}
	if isComponentDone(state.Status) {
		return
	}

	mode := inferComponentMode(component, state.Mode)
	switch mode {
	case "stop_loss":
		d.slTargets = append(d.slTargets, state.TargetPrice)
	case "take_profit":
		d.tpTargets = append(d.tpTargets, state.TargetPrice)
	}
}

func (d *exitDerivation) maybeSetEntrySide(entryPrice float64, side string) {
	if d.entry <= 0 && entryPrice > 0 {
		d.entry = entryPrice
	}
	if d.side == "" && strings.TrimSpace(side) != "" {
		d.side = strings.ToLower(strings.TrimSpace(side))
	}
}

func pickRootStopLoss(state exit.TierPlanState) float64 {
	switch {
	case state.FinalStopLoss > 0:
		return state.FinalStopLoss
	case state.StopLossPrice > 0:
		return state.StopLossPrice
	case state.TrailingStopPrice > 0:
		return state.TrailingStopPrice
	default:
		return 0
	}
}

func pickRootTakeProfit(state exit.TierPlanState) float64 {
	switch {
	case state.FinalTakeProfit > 0:
		return state.FinalTakeProfit
	case state.TakeProfitPrice > 0:
		return state.TakeProfitPrice
	default:
		return 0
	}
}

func isComponentDone(status string) bool {
	status = strings.ToLower(strings.TrimSpace(status))
	return status == "done" || status == "triggered"
}

func inferComponentMode(component string, mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != "" {
		return mode
	}
	lc := strings.ToLower(component)
	switch {
	case strings.Contains(lc, "sl") || strings.Contains(lc, "stop"):
		return "stop_loss"
	case strings.Contains(lc, "tp") || strings.Contains(lc, "take"):
		return "take_profit"
	default:
		return ""
	}
}

func (d *exitDerivation) finalize(currentPrice float64) derivedExitPrices {
	ref := currentPrice
	if ref <= 0 {
		ref = d.entry
	}
	out := derivedExitPrices{StopLoss: d.rootSL, TakeProfit: d.rootTP}
	if out.StopLoss <= 0 && len(d.slTargets) > 0 {
		out.StopLoss = pickNextStopLoss(d.slTargets, d.side, ref)
	}
	if out.TakeProfit <= 0 && len(d.tpTargets) > 0 {
		out.TakeProfit = pickNextTakeProfit(d.tpTargets, d.side, ref)
	}
	return out
}
