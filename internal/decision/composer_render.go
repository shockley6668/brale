package decision

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"brale/internal/logger"
	"brale/internal/market"
	formatutil "brale/internal/pkg/format"
	textutil "brale/internal/pkg/text"
	"brale/internal/types"
)

func (c *Composer) renderAccountOverview(account types.AccountSnapshot) string {
	var b strings.Builder
	if account.Total <= 0 && account.Available <= 0 && account.Used <= 0 {
		return ""
	}
	currency := strings.ToUpper(strings.TrimSpace(account.Currency))
	if currency == "" {
		currency = "USDT"
	}
	b.WriteString("\n## 账户资金\n")
	line := fmt.Sprintf("- 权益: %.2f %s", account.Total, currency)
	if account.Available > 0 {
		line += fmt.Sprintf(" · 可用: %.2f", account.Available)
	}
	if account.Used > 0 {
		line += fmt.Sprintf(" · 已使用: %.2f", account.Used)
	}
	b.WriteString(line + "\n")
	return b.String()
}

func (c *Composer) renderPreviousReasoning(reasonMap map[string]string) string {
	if len(reasonMap) == 0 {
		return ""
	}
	type entry struct {
		symbol string
		reason string
	}
	entries := make([]entry, 0, len(reasonMap))
	for sym, reason := range reasonMap {
		symbol := strings.ToUpper(strings.TrimSpace(sym))
		reason = strings.TrimSpace(reason)
		if symbol == "" || reason == "" {
			continue
		}
		entries = append(entries, entry{symbol: symbol, reason: reason})
	}
	if len(entries) == 0 {
		return ""
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].symbol < entries[j].symbol
	})
	var b strings.Builder
	b.WriteString("\n## 上一轮决策回顾\n")
	for _, ent := range entries {
		b.WriteString(fmt.Sprintf("- %s：%s\n", ent.symbol, textutil.Truncate(ent.reason, 1000)))
	}
	return b.String()
}

func (c *Composer) renderDerivativesMetrics(ctxs []string, directives map[string]ProfileDirective) string {
	if c.Metrics == nil || len(ctxs) == 0 || len(directives) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n## 市场衍生品数据 (Market Derivatives Data)\n")
	for _, sym := range ctxs {
		dir, ok := lookupDirective(sym, directives)
		if !ok || !dir.allowDerivatives() {
			continue
		}
		metricsData, ok := c.Metrics.Get(sym)
		if !ok || metricsData.Error != "" {
			b.WriteString(fmt.Sprintf("- %s: 获取衍生品数据失败 (%s)\n", strings.ToUpper(sym), metricsData.Error))
			continue
		}

		b.WriteString(fmt.Sprintf("- %s:\n", strings.ToUpper(sym)))
		if dir.IncludeOI {
			b.WriteString(fmt.Sprintf("  - 最新未平仓量 (OI): %.2f\n", metricsData.OI))
			for _, tf := range c.Metrics.GetTargetTimeframes() {
				if oldOI, ok := metricsData.OIHistory[tf]; ok && oldOI > 0 {
					changePct := (metricsData.OI - oldOI) / oldOI * 100
					b.WriteString(fmt.Sprintf("    - OI %s前: %.2f (%.2f%%)\n", tf, oldOI, changePct))
				} else {
					b.WriteString(fmt.Sprintf("    - OI %s前: 无数据\n", tf))
				}
			}
		}
		if dir.IncludeFunding {
			b.WriteString(fmt.Sprintf("  - 资金费率 (Funding Rate): %.4f%%\n", metricsData.FundingRate*100))
		}
	}
	b.WriteString("请结合这些衍生品数据评估市场情绪和资金动向。\n")
	return b.String()
}

func (c *Composer) renderPositionDetails(positions []types.PositionSnapshot) string {
	if len(positions) == 0 {
		return "\n## 当前持仓\n当前无持仓，只可返回 hold open_long open_short指令。\n"
	}
	var b strings.Builder
	b.WriteString("\n## 当前持仓\n")
	for _, pos := range positions {
		line := fmt.Sprintf("- %s %s entry=%.4f",
			strings.ToUpper(pos.Symbol), strings.ToUpper(pos.Side), pos.EntryPrice)
		if pos.Stake > 0 {
			line += fmt.Sprintf(" stake=%.2f", pos.Stake)
		}
		if pos.Leverage > 0 {
			line += fmt.Sprintf(" lev=x%.2f", pos.Leverage)
		}
		if pos.CurrentPrice > 0 {
			line += fmt.Sprintf(" last=%.4f", pos.CurrentPrice)
		}
		if pos.HoldingMs > 0 {
			line += fmt.Sprintf(" holding=%s", formatutil.Duration(pos.HoldingMs))
		}
		b.WriteString(line + "\n")
		if len(pos.PlanSummaries) > 0 {
			for _, note := range pos.PlanSummaries {
				msg := strings.TrimSpace(note)
				if msg == "" {
					continue
				}
				b.WriteString("    → " + msg + "\n")
			}
		}
		if jsonText := strings.TrimSpace(pos.PlanStateJSON); jsonText != "" {
			if planText := renderPlanStateSummary(jsonText); planText != "" {
				b.WriteString(planText)
				b.WriteString("    提示：只可修改 waiting 阶段的字段，triggered 的阶段请原值返回。\n")
			} else {
				b.WriteString("    ⚠️ 无法解析策略结构，以下为原始 JSON：\n")
				b.WriteString("    exit_plan_state_json:\n")
				lines := strings.Split(jsonText, "\n")
				for _, line := range lines {
					b.WriteString("        " + line + "\n")
				}
			}
		}
	}
	b.WriteString("请结合上述仓位判断是否需要平仓、加仓或调整计划。\n")
	return b.String()
}

func (c *Composer) renderKlineWindows(ctxs []AnalysisContext) string {
	if len(ctxs) == 0 {
		return ""
	}
	rank := buildIntervalRank(c.Intervals)
	windows := make([]klineWindow, 0, len(ctxs))
	var latestLine string
	for _, ac := range ctxs {
		csvData := strings.TrimSpace(ac.KlineCSV)
		bars, err := parseRecentCandles(ac.KlineJSON, priceWindowBars)
		if err != nil {
			logger.Debugf("kline snapshot 解析失败 %s %s: %v", ac.Symbol, ac.Interval, err)
			continue
		}
		if len(bars) == 0 && csvData == "" {
			continue
		}
		win := klineWindow{
			Symbol:   strings.ToUpper(strings.TrimSpace(ac.Symbol)),
			Interval: strings.TrimSpace(ac.Interval),
			Horizon:  strings.TrimSpace(ac.ForecastHorizon),
			Trend:    ac.TrendReport,
			CSV:      csvData,
			Bars:     bars,
		}
		if win.Symbol == "" || win.Interval == "" {
			continue
		}
		if len(bars) > 0 && latestLine == "" {
			latest := bars[len(bars)-1]
			latestLine = fmt.Sprintf("最新价格：%s 收=%.4f 时间=%s",
				strings.ToUpper(strings.TrimSpace(ac.Symbol)),
				latest.Close,
				time.UnixMilli(latest.CloseTime).UTC().Format(time.RFC3339))
		}
		windows = append(windows, win)
	}
	if len(windows) == 0 {
		return ""
	}
	sort.Slice(windows, func(i, j int) bool {
		if windows[i].Symbol == windows[j].Symbol {
			ri := intervalRankValue(windows[i].Interval, rank)
			rj := intervalRankValue(windows[j].Interval, rank)
			if ri != rj {
				return ri < rj
			}
			return windows[i].Interval < windows[j].Interval
		}
		return windows[i].Symbol < windows[j].Symbol
	})
	var b strings.Builder
	b.WriteString("\n## Price Windows（最近 4 根，最新在前）\n")
	if latestLine != "" {
		b.WriteString(latestLine + "\n")
	}
	for _, win := range windows {
		header := fmt.Sprintf("- %s %s", win.Symbol, win.Interval)
		if win.Horizon != "" {
			header += fmt.Sprintf(" (%s)", win.Horizon)
		}
		b.WriteString(header + "\n")
		if len(win.Bars) == 0 {
			continue
		}
		for idx := len(win.Bars) - 1; idx >= 0; idx-- {
			bar := win.Bars[idx]
			b.WriteString(fmt.Sprintf("    [%s, o=%s, h=%s, l=%s, c=%s, v=%.2f]\n",
				bar.TimeString(),
				formatutil.Float(bar.Open, 4),
				formatutil.Float(bar.High, 4),
				formatutil.Float(bar.Low, 4),
				formatutil.Float(bar.Close, 4),
				bar.Volume,
			))
		}
		if summary := market.Candles(win.Bars).Snapshot(win.Interval, win.Trend); summary != "" {
			b.WriteString("  Snapshot: " + summary + "\n")
		}
		b.WriteString("\n")
	}
	b.WriteString("请结合这些时间窗口评估当前价格位置与动量。\n")
	return b.String()
}

func (c *Composer) renderAgentBlocks(insights []AgentInsight) string {
	if len(insights) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n## Multi-Agent 协作\n")
	stageOrder := []string{agentStageIndicator, agentStagePattern, agentStageTrend}
	stageMap := make(map[string]AgentInsight, len(insights))
	for _, ins := range insights {
		if ins.Stage == "" {
			continue
		}
		stageMap[ins.Stage] = ins
	}
	write := func(ins AgentInsight) {
		if ins.Stage == "" {
			return
		}
		title := formatAgentStageTitle(ins.Stage)
		provider := strings.TrimSpace(ins.ProviderID)
		if provider == "" {
			provider = "-"
		}
		header := fmt.Sprintf("- [%s | 模型:%s] ", title, provider)
		output := strings.TrimSpace(ins.Output)
		if output != "" {
			b.WriteString(header)
			b.WriteString(textutil.Truncate(output, 3600))
			b.WriteString("\n")
			return
		}
		status := "无输出"
		if errTxt := strings.TrimSpace(ins.Error); errTxt != "" {
			status = errTxt
		}
		if ins.Warned {
			status += "（已通知）"
		}
		b.WriteString(header + status + "\n")
	}
	used := make(map[string]bool, len(stageOrder))
	for _, stage := range stageOrder {
		if ins, ok := stageMap[stage]; ok {
			write(ins)
			used[stage] = true
		}
	}
	for _, ins := range insights {
		if used[ins.Stage] {
			continue
		}
		write(ins)
	}
	b.WriteString("↑ 若 Agent 提示某组件已触发，保持现有退出计划，仅对未触发段位进行增改。\n")
	return b.String()
}

func (c *Composer) renderOutputConstraints(input Context) string {
	return renderOutputConstraints(input.ProfilePrompts, "只可返回json，不用输出其他内容。示例:")
}

func (c *Composer) logStructuredBlocksDebug(ctxs []AnalysisContext) {
	if !c.DebugStructuredBlocks || len(ctxs) == 0 {
		return
	}

	logger.Debugf("DebugStructuredBlocks: count=%d", len(ctxs))
}
