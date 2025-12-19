package freqtrade

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"brale/internal/gateway/exchange"
	"brale/internal/gateway/notifier"
	"brale/internal/logger"
	"brale/internal/trader"
)

func (m *Manager) sendEntryFillNotification(ctx context.Context, msg exchange.WebhookMessage, payload trader.PositionOpenedPayload) {
	if m == nil || m.notifier == nil {
		return
	}
	symbol := strings.ToUpper(strings.TrimSpace(freqtradePairToSymbol(payload.Symbol)))
	if symbol == "" {
		symbol = strings.ToUpper(strings.TrimSpace(freqtradePairToSymbol(msg.Pair)))
	}
	if symbol == "" {
		symbol = strings.ToUpper(strings.TrimSpace(msg.Pair))
	}
	tradeID, _ := strconv.Atoi(strings.TrimSpace(payload.TradeID))
	side := strings.ToLower(strings.TrimSpace(payload.Side))
	if side == "" {
		side = strings.ToLower(strings.TrimSpace(msg.Direction))
	}

	lines := []string{
		fmt.Sprintf("方向 %s · 杠杆 x%.0f", strings.ToUpper(side), payload.Leverage),
		fmt.Sprintf("成交价 %.4f", payload.Price),
	}
	if payload.Amount > 0 {
		lines = append(lines, fmt.Sprintf("数量 %.4f", payload.Amount))
	}
	if payload.Stake > 0 {
		lines = append(lines, fmt.Sprintf("仓位(USD) %.2f", payload.Stake))
	}

	if m.posRepo != nil && tradeID > 0 {
		recs, err := m.posRepo.ListStrategyInstances(ctx, tradeID)
		if err == nil && len(recs) > 0 {
			derived := deriveExitPricesFromStrategyInstances(recs, side, payload.Price)
			if derived.StopLoss > 0 {
				lines = append(lines, fmt.Sprintf("SL %.4f", derived.StopLoss))
			}
			if derived.TakeProfit > 0 {
				lines = append(lines, fmt.Sprintf("TP %.4f", derived.TakeProfit))
			}
		}
	}
	if tradeID > 0 {
		lines = append(lines, fmt.Sprintf("TradeID %d", tradeID))
	}

	msgBody := notifier.StructuredMessage{
		Icon:      "🚀",
		Title:     fmt.Sprintf("开仓完成：%s", symbol),
		Sections:  []notifier.MessageSection{{Title: "执行明细", Lines: lines}},
		Timestamp: time.Now().UTC(),
	}
	if err := m.notifier.SendText(msgBody.RenderMarkdown()); err != nil {
		logger.Warnf("Telegram 推送失败(entry_fill): %v", err)
	}
}

func (m *Manager) sendExitFillNotification(ctx context.Context, msg exchange.WebhookMessage, payload trader.PositionClosedPayload) {
	if m == nil || m.notifier == nil {
		return
	}
	symbol := strings.ToUpper(strings.TrimSpace(payload.Symbol))
	if symbol == "" {
		symbol = strings.ToUpper(strings.TrimSpace(msg.Pair))
	}
	tradeID, _ := strconv.Atoi(strings.TrimSpace(payload.TradeID))
	stageTitle, stageDetail := decodePlanReason(payload.Reason)
	title := fmt.Sprintf("平仓完成：%s", symbol)
	if stageTitle != "" {
		title = fmt.Sprintf("%s %s", title, stageTitle)
	}
	lines := m.buildExitNotificationLines(ctx, payload, symbol, tradeID, stageDetail)

	msgBody := notifier.StructuredMessage{
		Icon:      "🏁",
		Title:     title,
		Sections:  []notifier.MessageSection{{Title: "执行明细", Lines: lines}},
		Timestamp: time.Now().UTC(),
	}
	if err := m.notifier.SendText(msgBody.RenderMarkdown()); err != nil {
		logger.Warnf("Telegram 推送失败(exit_fill): %v", err)
	}
}

func (m *Manager) buildExitNotificationLines(ctx context.Context, payload trader.PositionClosedPayload, symbol string, tradeID int, stageDetail string) []string {
	lines := []string{fmt.Sprintf("成交价 %.4f", payload.ClosePrice)}
	if payload.Amount > 0 {
		lines = append(lines, fmt.Sprintf("本次成交 %.4f", payload.Amount))
	}
	if payload.RemainingAmount > 0 {
		lines = append(lines, fmt.Sprintf("剩余仓位 %.4f", payload.RemainingAmount))
	} else if payload.Amount > 0 {
		lines = append(lines, "剩余仓位 0 · 计划持仓已清空")
	}
	if stageDetail != "" {
		lines = append(lines, "策略阶段 "+stageDetail)
	} else if strings.TrimSpace(payload.Reason) != "" {
		lines = append(lines, "策略阶段 "+strings.TrimSpace(payload.Reason))
	}

	pnlAbs, pnlPct, pctAlreadyPercent := payload.PnL, payload.PnLPct, false
	if math.Abs(pnlAbs) < 1e-9 && math.Abs(pnlPct) < 1e-9 {
		entry := m.lookupEntryPrice(ctx, tradeID, symbol)
		side := m.lookupPositionSide(symbol, strings.ToLower(strings.TrimSpace(payload.Side)))
		if entry > 0 && payload.Amount > 0 {
			direction := 1.0
			if side == "short" {
				direction = -1
			}
			pnlAbs = (payload.ClosePrice - entry) * payload.Amount * direction
			pnlPct = (payload.ClosePrice - entry) / entry * 100 * direction
			pctAlreadyPercent = true
		}
	}

	if math.Abs(pnlAbs) >= 1e-9 || math.Abs(pnlPct) >= 1e-9 {
		lines = append(lines, formatPnLLine(pnlAbs, pnlPct, pctAlreadyPercent))
	}
	if tradeID > 0 {
		lines = append(lines, fmt.Sprintf("TradeID %d", tradeID))
	}
	return lines
}

func formatPnLLine(pnlAbs, pnlPct float64, pctAlreadyPercent bool) string {
	displayPct := pnlPct
	if !pctAlreadyPercent && math.Abs(displayPct) <= 2 {
		displayPct = displayPct * 100
	}
	// Example: "盈亏 +12.34 · +5.67%"
	return fmt.Sprintf("盈亏 %s · %s", formatSignedValue(pnlAbs), formatSignedPercent(displayPct))
}

func (m *Manager) lookupEntryPrice(ctx context.Context, tradeID int, symbol string) float64 {
	if ctx == nil {
		ctx = context.Background()
	}
	if tradeID > 0 && m.posRepo != nil {
		if rec, ok, err := m.posRepo.GetPosition(ctx, tradeID); err == nil && ok {
			if rec.Price != nil && *rec.Price > 0 {
				return *rec.Price
			}
		}
	}
	if m.trader != nil {
		snap := m.trader.Snapshot()
		if snap != nil {
			key := freqtradePairToSymbol(symbol)
			if key == "" {
				key = strings.ToUpper(strings.TrimSpace(symbol))
			}
			if pos, ok := snap.Positions[key]; ok && pos != nil && pos.EntryPrice > 0 {
				return pos.EntryPrice
			}
		}
	}
	return 0
}

func (m *Manager) lookupPositionSide(symbol string, fallback string) string {
	if m != nil && m.trader != nil {
		snap := m.trader.Snapshot()
		if snap != nil {
			key := freqtradePairToSymbol(symbol)
			if key == "" {
				key = strings.ToUpper(strings.TrimSpace(symbol))
			}
			if pos, ok := snap.Positions[key]; ok && pos != nil && pos.Side != "" {
				switch strings.ToLower(pos.Side) {
				case "short", "sell":
					return "short"
				default:
					return "long"
				}
			}
		}
	}
	switch strings.ToLower(strings.TrimSpace(fallback)) {
	case "short", "sell":
		return "short"
	default:
		return "long"
	}
}
