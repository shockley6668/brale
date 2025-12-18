package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"brale/internal/decision"
	"brale/internal/logger"
	"brale/internal/pkg/jsonutil"
	"brale/internal/pkg/text"
	livehttp "brale/internal/transport/http/live"
)

// latestPrice returns the latest price from the monitor.
func (s *LiveService) latestPrice(ctx context.Context, symbol string) float64 {
	return s.monitor.LatestPrice(ctx, symbol)
}

// PlanScheduler exposes the internal plan scheduler.
func (s *LiveService) PlanScheduler() *PlanScheduler {
	if s == nil {
		return nil
	}
	return s.planScheduler
}

// Close releases resources held by LiveService.
func (s *LiveService) Close() {
	if s == nil {
		return
	}
	if s.monitor != nil {
		s.monitor.Close()
	}
	if s.decLogs != nil {
		_ = s.decLogs.Close()
	}
	if s.strategyCloser != nil {
		_ = s.strategyCloser.Close()
	}
}

func (s *LiveService) logModelOutput(res decision.DecisionResult) {
	renderBlock := func(title, raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		_, start, ok := jsonutil.ExtractJSONWithOffset(raw)
		if ok {
			cot := strings.TrimSpace(raw[:start])
			cot = text.Truncate(cot, 4800)
			logger.Infof("")
			logger.InfoBlock(decision.RenderBlockTable(title, cot))
			return
		}
		logger.Infof("")
		logger.InfoBlock(decision.RenderBlockTable(title, "失败"))
	}
	if len(res.SymbolResults) > 0 {
		for _, block := range res.SymbolResults {
			title := fmt.Sprintf("AI[%s] 思维链", strings.ToUpper(strings.TrimSpace(block.Symbol)))
			if strings.TrimSpace(block.Symbol) == "" {
				title = "AI[final] 思维链"
			}
			renderBlock(title, block.RawOutput)
		}
		return
	}
	renderBlock("AI[final] 思维链", res.RawOutput)
}

func (s *LiveService) applyTradingDefaults(d *decision.Decision) {
	if d == nil {
		return
	}
	if d.Action != "open_long" && d.Action != "open_short" {
		return
	}
	if d.Leverage <= 0 {
		if def := s.cfg.Trading.DefaultLeverage; def > 0 {
			logger.Debugf("Decision %s missing leverage, using default %dx", d.Symbol, def)
			d.Leverage = def
		}
	}
	if d.PositionSizeUSD <= 0 {
		if size := s.cfg.Trading.PositionSizeUSD(); size > 0 {
			logger.Debugf("Decision %s missing position_size_usd, using default %.2f USDT", d.Symbol, size)
			d.PositionSizeUSD = size
		}
	}
}

func (s *LiveService) tradeIDForSymbol(symbol string) (int, error) {
	if s.execManager == nil {
		return 0, fmt.Errorf("freqtrade manager 未初始化")
	}
	id, ok := s.execManager.TradeIDBySymbol(symbol)
	if !ok || id <= 0 {
		return 0, fmt.Errorf("%s 无对应持仓 trade_id", strings.ToUpper(strings.TrimSpace(symbol)))
	}
	return id, nil
}

func (s *LiveService) logDecision(d decision.Decision) {
	lines := make([]string, 0, 8)
	actionCN := renderActionCN(d.Action)
	if actionCN == "" {
		actionCN = d.Action
	}
	lines = append(lines, fmt.Sprintf("AI 决策：%s %s", strings.ToUpper(strings.TrimSpace(d.Symbol)), actionCN))
	switch d.Action {
	case "open_long", "open_short":
		if d.Leverage > 0 || d.PositionSizeUSD > 0 {
			lines = append(lines, fmt.Sprintf("杠杆 %dx · 名义仓位 %.0f USDT", d.Leverage, d.PositionSizeUSD))
		}
	case "close_long", "close_short":
		if d.CloseRatio > 0 {
			lines = append(lines, fmt.Sprintf("本次平仓比例：%.2f%%", d.CloseRatio*100))
		}
	}
	if d.Confidence > 0 {
		lines = append(lines, fmt.Sprintf("模型信心：%d%%", d.Confidence))
	}
	if reason := strings.TrimSpace(d.Reasoning); reason != "" {
		lines = append(lines, "触发理由："+reason)
	}
	side := deriveSide(d.Action)
	if plan := s.renderExitPlanSummary(d.ExitPlan, d.ExitPlanVersion, 0, side); plan != "" {
		lines = append(lines, plan)
	}
	logger.Infof("%s", strings.Join(lines, "\n"))
}

func (s *LiveService) freqtradeHandleDecision(ctx context.Context, traceID string, d decision.Decision) error {
	if s.execManager == nil {
		return nil
	}
	traceID = s.ensureTraceID(traceID)
	logger.Infof("freqtrade: 接收决策 trace=%s symbol=%s action=%s", traceID, strings.ToUpper(strings.TrimSpace(d.Symbol)), d.Action)
	var marketPrice float64
	if d.Action == "open_long" || d.Action == "open_short" {
		price := s.latestPrice(ctx, d.Symbol)
		if price <= 0 {
			err := fmt.Errorf("获取 %s 当前价格失败，无法开仓", strings.ToUpper(d.Symbol))
			logger.Warnf("freqtrade: %v", err)
			return err
		}
		planID := ""
		if d.ExitPlan != nil {
			planID = strings.TrimSpace(d.ExitPlan.ID)
		}
		logger.Infof("freqtrade: 价格就绪 trace=%s symbol=%s side=%s price=%.4f exit_plan=%s", traceID, strings.ToUpper(strings.TrimSpace(d.Symbol)), deriveSide(d.Action), price, planID)
		marketPrice = price
		traceID = s.execManager.CacheDecision(traceID, d)
	}
	if err := s.execManager.Execute(ctx, decision.DecisionInput{
		TraceID:     traceID,
		Decision:    d,
		MarketPrice: marketPrice,
	}); err != nil {
		logger.Errorf("freqtrade: 执行失败 trace=%s symbol=%s action=%s err=%v", traceID, strings.ToUpper(strings.TrimSpace(d.Symbol)), d.Action, err)
		return err
	}
	logger.Infof("freqtrade: 决策已提交 trace=%s symbol=%s action=%s", traceID, strings.ToUpper(strings.TrimSpace(d.Symbol)), d.Action)
	return nil
}

func (s *LiveService) ensureTraceID(raw string) string {
	id := strings.TrimSpace(raw)
	if id != "" {
		return id
	}
	return fmt.Sprintf("trace-%d", time.Now().UnixNano())
}

// Implement livehttp.FreqtradeWebhookHandler interface is in http_handlers.go
var _ livehttp.FreqtradeWebhookHandler = (*LiveService)(nil)
