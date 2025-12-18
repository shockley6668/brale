package engine

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"brale/internal/decision"
	"brale/internal/gateway/notifier"
	"brale/internal/logger"
)

const (
	entryFillTimeout = 11 * time.Minute
)

// Notifier handles external notifications (e.g. Telegram).
type Notifier interface {
	SendStructured(msg notifier.StructuredMessage) error
}

func (e *LiveEngine) notifyMetaSummary(res decision.DecisionResult) {
	if e.Notifier == nil || e.Config == nil || !strings.EqualFold(e.Config.AI.Aggregation, "meta") {
		return
	}
	if err := e.sendMetaSummaryTelegram(res); err != nil {
		logger.Warnf("Telegram push failed (meta): %v", err)
	}
}

func (e *LiveEngine) sendMetaSummaryTelegram(res decision.DecisionResult) error {
	if e.Notifier == nil {
		return nil
	}

	if bd := res.MetaBreakdown; bd != nil && len(bd.Symbols) > 0 {
		logger.Infof("Meta breakdown: %d symbols, sending structured format", len(bd.Symbols))
		sections := buildMetaBreakdownSections(res, bd)
		msg := notifier.StructuredMessage{
			Icon:      "🗳️",
			Title:     "Meta 聚合投票",
			Sections:  sections,
			Timestamp: time.Now().UTC(),
		}
		return e.Notifier.SendStructured(msg)
	}

	// Legacy parsing logic removed as requested.
	// If we don't have breakdown, we don't send anything.
	logger.Warnf("Meta breakdown missing or empty, skipping notification.")
	return nil
}

func buildMetaBreakdownSections(res decision.DecisionResult, bd *decision.MetaVoteBreakdown) []notifier.MessageSection {
	sections := make([]notifier.MessageSection, 0, len(bd.Symbols))

	// 遍历每个币种，生成独立的 section
	for _, sym := range bd.Symbols {
		title := strings.TrimSpace(sym.Symbol)
		if title == "" {
			title = "-"
		}
		// 添加投票统计：ETH/USDT（hold:2 / open_long:1）
		if votePart := renderMetaVotesInline(sym.Votes); votePart != "" {
			title = fmt.Sprintf("%s（%s）", title, votePart)
		}
		lines := make([]string, 0, len(sym.Providers)+1)
		// 列出每个 LLM 的动作
		for _, p := range sym.Providers {
			act := strings.Join(p.Actions, ", ")
			if strings.TrimSpace(act) == "" {
				act = "-"
			}
			id := strings.TrimSpace(p.ProviderID)
			if id == "" {
				id = "-"
			}
			// Format: "- provider: action"
			if shouldShowMetaProviderWeight(p.Weight) {
				lines = append(lines, fmt.Sprintf("- %s[%s]: %s", id, formatMetaWeight(p.Weight), act))
			} else {
				lines = append(lines, fmt.Sprintf("- %s: %s", id, act))
			}
		}
		// 添加该币种的结论
		finalAction := strings.TrimSpace(sym.FinalAction)
		if finalAction == "" {
			finalAction = "HOLD"
		}
		lines = append(lines, fmt.Sprintf("→ 结论：%s", strings.ToUpper(finalAction)))
		sections = append(sections, notifier.MessageSection{Title: title, Lines: lines})
	}

	return sections
}

func metaSummaryFirstLine(summary string) string {
	summary = strings.TrimSpace(strings.ReplaceAll(summary, "```", "'''"))
	if summary == "" {
		return ""
	}
	if idx := strings.Index(summary, "\n"); idx >= 0 {
		summary = summary[:idx]
	}
	summary = strings.TrimSpace(summary)
	summary = strings.TrimPrefix(summary, "Meta聚合：")
	return strings.TrimSpace(summary)
}

func renderMetaFinalActions(decisions []decision.Decision) string {
	if len(decisions) == 0 {
		return ""
	}
	if len(decisions) == 1 && decision.NormalizeAction(decisions[0].Action) == "hold" {
		return "最终执行：HOLD"
	}
	parts := make([]string, 0, len(decisions))
	for _, d := range decisions {
		act := decision.NormalizeAction(d.Action)
		if act == "" {
			continue
		}
		sym := strings.ToUpper(strings.TrimSpace(d.Symbol))
		if sym == "" {
			sym = "-"
		}
		parts = append(parts, fmt.Sprintf("%s %s", sym, act))
	}
	if len(parts) == 0 {
		return ""
	}
	return "最终执行：" + strings.Join(parts, " / ")
}

func renderMetaVotesInline(votes []decision.MetaActionVote) string {
	if len(votes) == 0 {
		return ""
	}
	parts := make([]string, 0, len(votes))
	for _, v := range votes {
		act := strings.TrimSpace(v.Action)
		if act == "" || v.Weight <= 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s:%s", act, formatMetaWeight(v.Weight)))
	}
	return strings.Join(parts, " / ")
}

func shouldShowMetaProviderWeight(weight float64) bool {
	if weight <= 0 {
		return false
	}
	// Only show when non-default to keep messages compact.
	return math.Abs(weight-1.0) > 1e-9
}

func formatMetaWeight(weight float64) string {
	if weight == 0 {
		return "0"
	}
	if math.Abs(weight-math.Round(weight)) <= 1e-9 {
		return fmt.Sprintf("%.0f", weight)
	}
	return fmt.Sprintf("%.2f", weight)
}

func (e *LiveEngine) notifyOpenAfterFill(ctx context.Context, d decision.Decision, fallbackPrice float64, validateIv string) {
	// If no executor, just notify with fallback
	// LiveEngine always has PosService, but maybe not ExecutionManager directly.
	// We rely on PosService.ListPositions to find entry price.

	symbol := strings.ToUpper(strings.TrimSpace(d.Symbol))
	if symbol == "" {
		e.notifyOpen(ctx, d, fallbackPrice, validateIv)
		return
	}
	go func(dec decision.Decision, sym string) {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		timeout := time.After(entryFillTimeout)
		for {
			select {
			case <-ctx.Done():
				return
			case <-timeout:
				logger.Warnf("Waiting for %s entry_fill timeout, sending timeout alert", sym)
				e.notifyEntryTimeout(ctx, dec)
				return
			case <-ticker.C:
				if entry := e.lookupEntryPrice(ctx, sym); entry > 0 {
					e.notifyOpen(ctx, dec, entry, validateIv)
					return
				}
			}
		}
	}(d, symbol)
}

func (e *LiveEngine) lookupEntryPrice(ctx context.Context, symbol string) float64 {
	positions, err := e.PosService.ListPositions(ctx)
	if err != nil {
		return 0
	}
	for _, p := range positions {
		if strings.EqualFold(p.Symbol, symbol) {
			return p.EntryPrice
		}
	}
	return 0
}

func (e *LiveEngine) notifyOpen(ctx context.Context, d decision.Decision, entryPrice float64, validateIv string) {
	if e.Notifier == nil {
		return
	}
	rrVal := 0.0
	if entryPrice > 0 {
		var risk, reward float64
		switch d.Action {
		case "open_long":
			risk = entryPrice - d.StopLoss
			reward = d.TakeProfit - entryPrice
		case "open_short":
			risk = d.StopLoss - entryPrice
			reward = entryPrice - d.TakeProfit
		}
		if risk > 0 && reward > 0 {
			rrVal = reward / risk
		}
	}

	if entryPrice > 0 {
		if rrVal > 0 {
			logger.Infof("开仓详情: %s %s entry=%.4f RR=%.2f sl=%.4f tp=%.4f",
				d.Symbol, d.Action, entryPrice, rrVal, d.StopLoss, d.TakeProfit)
		} else {
			logger.Infof("开仓详情: %s %s entry=%.4f sl=%.4f tp=%.4f",
				d.Symbol, d.Action, entryPrice, d.StopLoss, d.TakeProfit)
		}
	}

	actionCN := renderActionCN(d.Action)
	side := deriveSide(d.Action)
	if actionCN == "" {
		actionCN = d.Action
	}
	sections := make([]notifier.MessageSection, 0, 4)
	priceLines := make([]string, 0, 3)
	if entryPrice > 0 {
		iv := ""
		if validateIv != "" {
			iv = " · 周期 " + strings.ToUpper(validateIv)
		}
		priceLines = append(priceLines, fmt.Sprintf("当前价格 %.4f%s", entryPrice, iv))
	}
	if rrVal > 0 {
		priceLines = append(priceLines, fmt.Sprintf("即时风险回报：%.2f", rrVal))
	}
	if len(priceLines) > 0 {
		sections = append(sections, notifier.MessageSection{Title: "行情", Lines: priceLines})
	}
	tradeLines := make([]string, 0, 4)
	if d.Leverage > 0 {
		tradeLines = append(tradeLines, fmt.Sprintf("杠杆 %dx", d.Leverage))
	}
	if d.PositionSizeUSD > 0 {
		tradeLines = append(tradeLines, fmt.Sprintf("仓位 %.0f USDT", d.PositionSizeUSD))
	}
	if d.Confidence > 0 {
		tradeLines = append(tradeLines, fmt.Sprintf("模型信心 %d%%", d.Confidence))
	}
	if len(tradeLines) > 0 {
		sections = append(sections, notifier.MessageSection{Title: "仓位", Lines: tradeLines})
	}

	// Exit Plan logic requires ExitPlans registry or similar.
	// LiveEngine has ExitPolicy which has ExitPlans.
	if plan := e.renderExitPlanSummary(d.ExitPlan, d.ExitPlanVersion, entryPrice, side); plan != "" {
		planLines := strings.Split(plan, "\n")
		sections = append(sections, notifier.MessageSection{Title: "策略", Lines: planLines})
		logger.Infof("策略详情：\n%s", plan)
	}

	if reason := strings.TrimSpace(d.Reasoning); reason != "" {
		reasonLines := strings.Split(reason, "\n")
		sections = append(sections, notifier.MessageSection{Title: "触发理由", Lines: reasonLines})
	}
	msg := notifier.StructuredMessage{
		Icon:      "🚀",
		Title:     fmt.Sprintf("信号触发：%s %s", strings.ToUpper(strings.TrimSpace(d.Symbol)), actionCN),
		Sections:  sections,
		Timestamp: time.Now().UTC(),
	}
	if err := e.Notifier.SendStructured(msg); err != nil {
		logger.Warnf("Telegram 推送失败: %v", err)
	}
}

func (e *LiveEngine) notifyEntryTimeout(ctx context.Context, d decision.Decision) {
	if e.Notifier == nil {
		return
	}
	actionCN := renderActionCN(d.Action)
	if actionCN == "" {
		actionCN = d.Action
	}
	lines := []string{
		"已等待超过 11 分钟仍未收到交易所 entry_fill 回执，可能尚未成交或被拒单。",
		"请检查交易所委托状态，必要时手动撤单/重试。",
	}
	msg := notifier.StructuredMessage{
		Icon:      "⏱️",
		Title:     fmt.Sprintf("下单超时：%s %s", strings.ToUpper(strings.TrimSpace(d.Symbol)), actionCN),
		Sections:  []notifier.MessageSection{{Title: "提醒", Lines: lines}},
		Timestamp: time.Now().UTC(),
	}
	if err := e.Notifier.SendStructured(msg); err != nil {
		logger.Warnf("Telegram 推送失败(timeout): %v", err)
	}
}

func renderActionCN(action string) string {
	switch action {
	case "open_long":
		return "开多"
	case "open_short":
		return "开空"
	case "close_long":
		return "平多"
	case "close_short":
		return "平空"
	case "hold", "wait":
		return "观望"
	case "update_exit_plan":
		return "更新策略"
	default:
		return ""
	}
}

func deriveSide(action string) string {
	switch action {
	case "open_long", "close_long":
		return "long"
	case "open_short", "close_short":
		return "short"
	default:
		return ""
	}
}

func (e *LiveEngine) renderExitPlanSummary(spec *decision.ExitPlanSpec, version int, entryPrice float64, side string) string {
	if spec == nil || strings.TrimSpace(spec.ID) == "" {
		return ""
	}
	label := strings.TrimSpace(spec.ID)
	if e.ExitPlans != nil {
		if tpl, ok := e.ExitPlans.Template(label); ok {
			label = tpl.ID
			if version <= 0 {
				version = tpl.Version
			}
		}
	}
	var builder strings.Builder
	builder.WriteString("策略：")
	if version > 0 {
		builder.WriteString(fmt.Sprintf("%s (v%d)", label, version))
	} else {
		builder.WriteString(label)
	}
	paramLines := summarizePlanParams(spec.Params, entryPrice, side)
	if len(paramLines) > 0 {
		builder.WriteString("\n" + strings.Join(paramLines, "\n"))
	}
	return builder.String()
}
