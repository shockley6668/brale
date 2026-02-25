package freqtrade

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"brale/internal/decision"
	"brale/internal/gateway/exchange"
	"brale/internal/trader"
)

func buildSignalEntryPayload(d decision.Decision, side string, entryPrice float64) trader.SignalEntryPayload {
	sp := trader.SignalEntryPayload{
		Order: exchange.OpenRequest{
			Symbol: d.Symbol,
			Side:   side,

			OrderType: "limit",
			Price:     entryPrice,
			Amount:    d.PositionSizeUSD,
		},
	}
	if d.Leverage > 0 {
		sp.Order.Leverage = float64(d.Leverage)
	}
	return sp
}

func (m *Manager) Execute(ctx context.Context, input decision.DecisionInput) error {
	if m == nil || m.trader == nil {
		return fmt.Errorf("trader actor not initialized")
	}
	d := input.Decision
	payload, err := json.Marshal(d)
	if err != nil {
		return err
	}

	var evtType trader.EventType
	switch d.Action {
	case "open_long", "open_short":
		evtType = trader.EvtSignalEntry
	case "close_long", "close_short":
		evtType = trader.EvtSignalExit
	case "update_exit_plan":
		return nil
	default:
		return nil
	}

	if evtType == trader.EvtSignalEntry {
		side := "long"
		if d.Action == "open_short" {
			side = "short"
		}
		entryPrice := m.effectiveEntryPrice(side, input.MarketPrice)
		if entryPrice <= 0 {
			return fmt.Errorf("无效 market price，无法开仓")
		}
		if err := m.validateInitialStopDistance(d, side, entryPrice); err != nil {
			return err
		}
		sp := buildSignalEntryPayload(d, side, entryPrice)
		if p, err := json.Marshal(sp); err == nil {
			payload = p
		}
	}

	eventID := managerEventID(input.TraceID, "decision")
	if err := m.trader.Send(trader.EventEnvelope{
		ID:        eventID,
		Type:      evtType,
		Payload:   payload,
		CreatedAt: time.Now(),
		Symbol:    strings.ToUpper(strings.TrimSpace(d.Symbol)),
	}); err != nil {
		return err
	}
	return nil
}
