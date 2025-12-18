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

// HandleWebhook handles webhook requests to update position and logs.
func (m *Manager) HandleWebhook(ctx context.Context, msg exchange.WebhookMessage) {
	logger.Debugf("Freqtrade Webhook received: %s trade=%d", msg.Type, msg.TradeID)

	if m.trader == nil {
		return
	}

	var evtType trader.EventType
	var payload any
	var postSend func()
	delay := time.Duration(0)
	tradeID := int(msg.TradeID)

	msgType := strings.ToLower(strings.TrimSpace(msg.Type))
	switch msgType {
	case "entry":
		evtType = trader.EvtPositionOpening
		createdAt := parseFreqtradeTime(msg.OpenDate)
		if createdAt.IsZero() {
			createdAt = time.Now()
		}
		payload = trader.PositionOpeningPayload{
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
	case "entry_fill", "entry_fill_info":
		evtType = trader.EvtPositionOpened
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
		payload = openedPayload
		m.clearPending(tradeID, pendingStageOpening)
		delay = reconcileDelay
		postSend = func() {
			m.initExitPlanOnEntryFill(ctx, tradeID, msg.Pair, float64(msg.OpenRate))
			if m.notifier != nil {
				go m.sendEntryFillNotification(ctx, msg, openedPayload)
			}
		}
	case "exit":
		evtType = trader.EvtPositionClosing
		reqAt := parseFreqtradeTime(msg.CloseDate)
		if reqAt.IsZero() {
			reqAt = time.Now()
		}
		payload = trader.PositionClosingPayload{
			TradeID:   strconv.FormatInt(int64(msg.TradeID), 10),
			Symbol:    msg.Pair,
			Side:      msg.Direction,
			CreatedAt: reqAt,
		}
		m.startPending(tradeID, pendingStageClosing)
	case "exit_fill", "exit_fill_info":
		evtType = trader.EvtPositionClosed
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
		payload = closedPayload
		m.clearPending(tradeID, pendingStageClosing)
		delay = reconcileDelay
		postSend = func() {
			m.reconcileExitFillWithFreqtrade(ctx, msg, &closedPayload)
			if closedPayload.RemainingAmount <= 0.000001 {
				if err := m.posStore.FinalizeStrategies(ctx, int(msg.TradeID)); err != nil {
					logger.Warnf("Failed to finalize strategies for trade %d: %v", msg.TradeID, err)
				} else {
					logger.Infof("Finalized strategies for trade %d (Full Exit)", msg.TradeID)
				}
			} else {
				if err := m.posStore.FinalizePendingStrategies(ctx, int(msg.TradeID)); err != nil {
					logger.Warnf("Failed to finalize pending strategies for trade %d: %v", msg.TradeID, err)
				}
				logger.Infof("Finalized pending strategies for trade %d (Partial Exit, Remaining: %.4f)", msg.TradeID, closedPayload.RemainingAmount)
			}
			if m.planUpdateHook != nil {
				m.planUpdateHook.NotifyPlanUpdated(context.Background(), int(msg.TradeID))
			}
			if closedPayload.Amount > 0 && m.notifier != nil {
				go m.sendExitFillNotification(ctx, msg, closedPayload)
			}
		}
	default:
		return
	}

	data, err := json.Marshal(payload)
	if err != nil {
		logger.Warnf("Failed to marshal webhook payload: %v", err)
		return
	}

	m.trader.Send(trader.EventEnvelope{
		ID:        managerEventID("", "webhook"),
		Type:      evtType,
		Payload:   data,
		CreatedAt: time.Now(),
		TradeID:   tradeID,
		Symbol:    strings.ToUpper(strings.TrimSpace(msg.Pair)),
	})
	if delay > 0 {
		m.reconcileTradeAsyncWithDelay(tradeID, delay)
	} else if tradeID > 0 {
		m.reconcileTradeAsync(tradeID)
	}
	if postSend != nil {
		postSend()
	}
}
