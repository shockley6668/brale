package freqtrade

import (
	"context"
	"time"

	"brale/internal/decision"
	"brale/internal/logger"
)

func (m *Manager) Positions() []decision.PositionSnapshot {
	if m == nil {
		return nil
	}
	if m.trader != nil {
		snap := m.trader.Snapshot()
		return snapshotDecisionPositions(snap)
	}
	if m.posRepo == nil {
		return nil
	}
	recs, err := m.posRepo.ListActivePositions(context.Background(), 100)
	if err != nil {
		logger.Warnf("failed to list active positions: %v", err)
		return nil
	}
	var out []decision.PositionSnapshot
	now := time.Now().UnixMilli()
	for _, r := range recs {
		holdingMs := int64(0)
		if r.StartTime != nil {
			holdingMs = now - r.StartTime.UnixMilli()
		}
		out = append(out, decision.PositionSnapshot{
			Symbol:          r.Symbol,
			Side:            r.Side,
			Quantity:        valOrZero(r.Amount),
			EntryPrice:      valOrZero(r.Price),
			CurrentPrice:    valOrZero(r.CurrentPrice),
			UnrealizedPn:    valOrZero(r.UnrealizedPnLUSD),
			UnrealizedPnPct: valOrZero(r.UnrealizedPnLRatio) * 100,
			HoldingMs:       holdingMs,
			Leverage:        valOrZero(r.Leverage),
			Stake:           valOrZero(r.StakeAmount),
		})
	}
	return out
}
