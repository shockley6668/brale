package freqtrade

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"brale/internal/gateway/exchange"
	"brale/internal/logger"
	"brale/internal/pkg/convert"
	"brale/internal/trader"
)

func (m *Manager) HandleWebhook(ctx context.Context, msg exchange.WebhookMessage) {
	logger.Debugf("Freqtrade Webhook received: %s trade=%d", msg.Type, msg.TradeID)

	if m.trader == nil {
		return
	}

	tradeID := int(msg.TradeID)
	evt, ok := m.buildWebhookEvent(ctx, msg, tradeID)
	if !ok {
		return
	}

	data, err := json.Marshal(evt.payload)
	if err != nil {
		logger.Warnf("Failed to marshal webhook payload: %v", err)
		return
	}

	m.trader.Send(trader.EventEnvelope{
		ID:        managerEventID("", "webhook"),
		Type:      evt.evtType,
		Payload:   data,
		CreatedAt: time.Now(),
		TradeID:   tradeID,
		Symbol:    strings.ToUpper(strings.TrimSpace(msg.Pair)),
	})
	evt.afterSend()
}

type webhookEvent struct {
	evtType   trader.EventType
	payload   any
	afterSend func()
}

func (m *Manager) buildWebhookEvent(ctx context.Context, msg exchange.WebhookMessage, tradeID int) (webhookEvent, bool) {
	msgType := strings.ToLower(strings.TrimSpace(msg.Type))
	switch msgType {
	case "entry":
		return m.buildEntryOpeningEvent(msg, tradeID), true
	case "entry_fill", "entry_fill_info":
		return m.buildEntryFillEvent(ctx, msg, tradeID), true
	case "exit":
		return m.buildExitRequestEvent(msg, tradeID), true
	case "exit_fill", "exit_fill_info":
		return m.buildExitFillEvent(ctx, msg, tradeID), true
	default:
		return webhookEvent{}, false
	}
}

// buildEntryOpeningEvent produces an opening event and marks pending state.
func (m *Manager) buildEntryOpeningEvent(msg exchange.WebhookMessage, tradeID int) webhookEvent {
	createdAt := parseFreqtradeTime(msg.OpenDate)
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	payload := trader.PositionOpeningPayload{
		TradeID:   strconv.FormatInt(int64(msg.TradeID), 10),
		Symbol:    msg.Pair,
		Side:      msg.Direction,
		Stake:     float64(msg.StakeAmount),
		Leverage:  float64(msg.Leverage),
		Amount:    float64(msg.Amount),
		Price:     float64(msg.OpenRate),
		CreatedAt: createdAt,
	}
	m.startPending(tradeID, pendingStageOpening)
	return webhookEvent{evtType: trader.EvtPositionOpening, payload: payload, afterSend: func() {}}
}

func (m *Manager) buildEntryFillEvent(ctx context.Context, msg exchange.WebhookMessage, tradeID int) webhookEvent {
	openedAt := parseFreqtradeTime(msg.OpenDate)
	if openedAt.IsZero() {
		openedAt = time.Now()
	}
	openedPayload := trader.PositionOpenedPayload{
		TradeID:  strconv.FormatInt(int64(msg.TradeID), 10),
		Symbol:   msg.Pair,
		Side:     msg.Direction,
		Price:    float64(msg.OpenRate),
		Amount:   float64(msg.Amount),
		Stake:    float64(msg.StakeAmount),
		Leverage: float64(msg.Leverage),
		OpenedAt: openedAt,
	}
	m.clearPending(tradeID, pendingStageOpening)
	return webhookEvent{
		evtType: trader.EvtPositionOpened,
		payload: openedPayload,
		afterSend: func() {
			m.reconcileAfterDelay(tradeID)
			m.initExitPlanOnEntryFill(ctx, tradeID, msg.Pair, float64(msg.OpenRate))
			if m.notifier != nil {
				go m.sendEntryFillNotification(ctx, msg, openedPayload)
			}
		},
	}
}

func (m *Manager) buildExitRequestEvent(msg exchange.WebhookMessage, tradeID int) webhookEvent {
	reqAt := parseFreqtradeTime(msg.CloseDate)
	if reqAt.IsZero() {
		reqAt = time.Now()
	}
	payload := trader.PositionClosingPayload{
		TradeID:   strconv.FormatInt(int64(msg.TradeID), 10),
		Symbol:    msg.Pair,
		Side:      msg.Direction,
		CreatedAt: reqAt,
	}
	m.startPending(tradeID, pendingStageClosing)
	return webhookEvent{
		evtType:   trader.EvtPositionClosing,
		payload:   payload,
		afterSend: func() { m.reconcileAfterDelay(tradeID) },
	}
}

func (m *Manager) buildExitFillEvent(ctx context.Context, msg exchange.WebhookMessage, tradeID int) webhookEvent {
	closedAt := parseFreqtradeTime(msg.CloseDate)
	if closedAt.IsZero() {
		closedAt = time.Now()
	}
	profitAbs := convert.ToFloat64(msg.ProfitAbs)
	profitRatio := convert.ToFloat64(msg.ProfitRatio)
	reason := strings.TrimSpace(msg.ExitReason)
	if reason == "" {
		reason = strings.TrimSpace(msg.Reason)
	}
	executed := float64(msg.Amount)
	closedAmount, remaining := m.deriveCloseBreakdown(msg.Pair, executed)
	closedPayload := trader.PositionClosedPayload{
		TradeID:         strconv.FormatInt(int64(msg.TradeID), 10),
		Symbol:          msg.Pair,
		Side:            msg.Direction,
		Amount:          closedAmount,
		RemainingAmount: remaining,
		ClosePrice:      float64(msg.CloseRate),
		Reason:          reason,
		PnL:             profitAbs,
		PnLPct:          profitRatio,
		ClosedAt:        closedAt,
	}
	m.clearPending(tradeID, pendingStageClosing)

	afterSend := func() {
		m.reconcileAfterDelay(tradeID)
		m.finalizeStrategiesOnExit(ctx, msg, closedPayload)
		if closedPayload.Amount > 0 && m.notifier != nil {
			go m.sendExitFillNotification(ctx, msg, closedPayload)
		}
	}
	return webhookEvent{evtType: trader.EvtPositionClosed, payload: closedPayload, afterSend: afterSend}
}

func (m *Manager) reconcileAfterDelay(tradeID int) {
	if tradeID <= 0 {
		return
	}
	m.reconcileTradeAsyncWithDelay(tradeID, reconcileDelay)
}

// finalizeStrategiesOnExit handles finalization depending on remaining amount.
// Example: If RemainingAmount=0 => finalize strategies; otherwise finalize pending only.
func (m *Manager) finalizeStrategiesOnExit(ctx context.Context, msg exchange.WebhookMessage, payload trader.PositionClosedPayload) {
	if payload.RemainingAmount <= 0.000001 {
		if err := m.posStore.FinalizeStrategies(ctx, int(msg.TradeID)); err != nil {
			logger.Warnf("Failed to finalize strategies for trade %d: %v", msg.TradeID, err)
		} else {
			logger.Infof("Finalized strategies for trade %d (Full Exit)", msg.TradeID)
		}
	} else {
		if err := m.posStore.FinalizePendingStrategies(ctx, int(msg.TradeID)); err != nil {
			logger.Warnf("Failed to finalize pending strategies for trade %d: %v", msg.TradeID, err)
		}
		logger.Infof("Finalized pending strategies for trade %d (Partial Exit, Remaining: %.4f)", msg.TradeID, payload.RemainingAmount)
	}
	if m.planUpdateHook != nil {
		m.planUpdateHook.NotifyPlanUpdated(context.Background(), int(msg.TradeID))
	}
}
