package decision

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"brale/internal/logger"
	"brale/internal/market"
	formatutil "brale/internal/pkg/format"
	jsonutil "brale/internal/pkg/jsonutil"
	textutil "brale/internal/pkg/text"
)

func (b *DefaultPromptBuilder) renderAgentBlocks(insights []AgentInsight) string {
	if len(insights) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n## Multi-Agent 协作\n")
	stageOrder := []string{agentStageIndicator, agentStagePattern, agentStageTrend, agentStageMechanics}
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
			sb.WriteString(header)
			sb.WriteString(textutil.Truncate(output, 3600))
			sb.WriteString("\n")
			return
		}
		status := "无输出"
		if errTxt := strings.TrimSpace(ins.Error); errTxt != "" {
			status = errTxt
		}
		if ins.Warned {
			status += "（已通知）"
		}
		sb.WriteString(header + status + "\n")
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
	//sb.WriteString("↑ 若 Agent 提示某组件已触发，保持现有退出计划，仅对未触发段位进行增改。\n")
	return sb.String()
}

func (b *DefaultPromptBuilder) renderPreviousReasoning(reasonMap map[string]string) string {
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
	var sb strings.Builder
	sb.WriteString("\n## 上一轮决策回顾\n")
	for _, ent := range entries {
		sb.WriteString(fmt.Sprintf("- %s：%s\n", ent.symbol, textutil.Truncate(ent.reason, 1000)))
	}
	return sb.String()
}

func (b *DefaultPromptBuilder) renderPreviousProviderOutputs(outputs []ProviderOutputSnapshot) string {
	if len(outputs) == 0 {
		return ""
	}
	sort.Slice(outputs, func(i, j int) bool {
		return strings.TrimSpace(outputs[i].ProviderID) < strings.TrimSpace(outputs[j].ProviderID)
	})
	var sb strings.Builder
	sb.WriteString("\n## 上一轮多模型输出\n")
	for _, out := range outputs {
		provider := strings.TrimSpace(out.ProviderID)
		if provider == "" {
			provider = "-"
		}
		summary := formatProviderOutputSummary(out)
		sb.WriteString(fmt.Sprintf("- %s: %s\n", provider, summary))
	}
	return sb.String()
}

func formatProviderOutputSummary(out ProviderOutputSnapshot) string {
	if len(out.Decisions) == 0 {
		raw := strings.TrimSpace(out.RawOutput)
		if raw == "" {
			return "无有效决策"
		}
		return fmt.Sprintf("无有效决策，原始输出：%s", textutil.Truncate(raw, 800))
	}
	parts := make([]string, 0, len(out.Decisions))
	for _, d := range out.Decisions {
		action := strings.TrimSpace(d.Action)
		if action == "" {
			action = "unknown"
		}
		symbol := strings.ToUpper(strings.TrimSpace(d.Symbol))
		reason := strings.TrimSpace(d.Reasoning)
		if reason == "" {
			reason = "无理由"
		} else {
			reason = textutil.Truncate(reason, 800)
		}
		if symbol != "" {
			parts = append(parts, fmt.Sprintf("action=%s symbol=%s reasoning=%s", action, symbol, reason))
			continue
		}
		parts = append(parts, fmt.Sprintf("action=%s reasoning=%s", action, reason))
	}
	return strings.Join(parts, " | ")
}

func (b *DefaultPromptBuilder) renderOutputConstraints(input Context) string {
	return renderOutputConstraints(input.ProfilePrompts, "只可以返回和示例一致格式的json数据，并且只可以有单个action\n reasoning在100字以内。示例:")
}

func (b *DefaultPromptBuilder) buildDerivativesSection(ctx context.Context, ctxs []AnalysisContext, directives map[string]ProfileDirective) (string, time.Time, string) {
	if len(ctxs) == 0 || len(directives) == 0 {
		return "", time.Time{}, ""
	}
	if b.Metrics == nil && b.FearGreed == nil && b.Store == nil && b.Sentiment == nil {
		return "", time.Time{}, ""
	}
	symbols := uniqueSymbols(ctxs)
	if len(symbols) == 0 {
		return "", time.Time{}, ""
	}
	intervalsBySymbol := groupIntervalsBySymbol(ctxs, buildIntervalRank(b.Intervals))

	var sb strings.Builder
	sb.WriteString("\n## 市场衍生品数据 (Market Derivatives Data)\n")
	var fgData market.FearGreedData
	fgOK := false
	if b.FearGreed != nil {
		fgData, fgOK = b.FearGreed.Get()
	}
	wantFearGreed := false
	latestUpdate := time.Time{}
	var fingerprintParts []string
	for _, sym := range symbols {
		dir, ok := lookupDirective(sym, directives)
		if !ok || !dir.DerivativesEnabled || !dir.IncludeFearGreed {
			continue
		}
		wantFearGreed = true
		break
	}
	if wantFearGreed {
		sb.WriteString("- 恐慌与贪婪指数 (Fear & Greed):\n")
		if !fgOK || fgData.Error != "" {
			errMsg := fgData.Error
			if errMsg == "" {
				errMsg = "无数据"
			}
			sb.WriteString(fmt.Sprintf("  - 获取失败 (%s)\n", errMsg))
		} else if len(fgData.History) == 0 {
			sb.WriteString("  - 无数据\n")
		} else {
			limit := 5
			if len(fgData.History) < limit {
				limit = len(fgData.History)
			}
			for i := 0; i < limit; i++ {
				point := fgData.History[i]
				ts := "-"
				if !point.Timestamp.IsZero() {
					ts = point.Timestamp.UTC().Format(time.RFC3339)
				}
				sb.WriteString(fmt.Sprintf("  - %s: %d (%s)\n", ts, point.Value, point.Classification))
			}
			if fgData.TimeUntilUpdate > 0 {
				sb.WriteString(fmt.Sprintf("  - 距下次更新=%s\n", fgData.TimeUntilUpdate.Round(time.Second)))
			}
			if fgData.LastUpdate.After(latestUpdate) {
				latestUpdate = fgData.LastUpdate
			}
			var fp strings.Builder
			fp.WriteString("fg:")
			for i := 0; i < limit; i++ {
				point := fgData.History[i]
				fmt.Fprintf(&fp, "%d:%s|", point.Value, point.Classification)
			}
			fingerprintParts = append(fingerprintParts, fp.String())
		}
	}

	for _, sym := range symbols {
		dir, ok := lookupDirective(sym, directives)
		if !ok || !dir.DerivativesEnabled {
			continue
		}
		sb.WriteString(fmt.Sprintf("- %s:\n", sym))

		var metricsData market.DerivativesData
		metricsOK := false
		if b.Metrics != nil {
			metricsData, metricsOK = b.Metrics.Get(sym)
		}
		if (dir.IncludeOI || dir.IncludeFunding) && (!metricsOK || metricsData.Error != "") {
			errMsg := metricsData.Error
			if errMsg == "" {
				errMsg = "无数据"
			}
			sb.WriteString(fmt.Sprintf("  - 衍生品数据获取失败 (%s)\n", errMsg))
		} else if b.Metrics != nil && (dir.IncludeOI || dir.IncludeFunding) {
			var fp strings.Builder
			fp.WriteString("sym=")
			fp.WriteString(sym)
			if dir.IncludeOI {
				sb.WriteString(fmt.Sprintf("  - 最新未平仓量 (OI): %.2f\n", metricsData.OI))
				fp.WriteString("|oi=")
				fp.WriteString(formatutil.Float(metricsData.OI, 4))
				for _, tf := range b.Metrics.GetTargetTimeframes() {
					if oldOI, ok := metricsData.OIHistory[tf]; ok && oldOI > 0 {
						changePct := (metricsData.OI - oldOI) / oldOI * 100
						sb.WriteString(fmt.Sprintf("    - OI %s前: %.2f (%.2f%%)\n", tf, oldOI, changePct))
						fp.WriteString("|oi_")
						fp.WriteString(tf)
						fp.WriteString("=")
						fp.WriteString(formatutil.Float(oldOI, 4))
					} else {
						sb.WriteString(fmt.Sprintf("    - OI %s前: 无数据\n", tf))
						fp.WriteString("|oi_")
						fp.WriteString(tf)
						fp.WriteString("=0")
					}
				}
			}
			if dir.IncludeFunding {
				sb.WriteString(fmt.Sprintf("  - 资金费率 (Funding Rate): %.4f%%\n", metricsData.FundingRate*100))
				fp.WriteString("|fund=")
				fp.WriteString(formatutil.Float(metricsData.FundingRate, 8))
			}
			if metricsData.LastUpdate.After(latestUpdate) {
				latestUpdate = metricsData.LastUpdate
			}
			if fp.Len() > 0 {
				fingerprintParts = append(fingerprintParts, fp.String())
			}
		}

		intervals := intervalsBySymbol[sym]
		if len(intervals) == 0 {
			continue
		}
		for _, iv := range intervals {
			sb.WriteString(fmt.Sprintf("  - %s:\n", iv))
			var candles []market.Candle
			if b.Store != nil {
				stored, err := b.Store.Get(ctx, sym, iv)
				if err == nil {
					candles = stored
				}
			}
			if len(candles) == 0 {
				sb.WriteString("    - CVD: 无数据\n")
				sb.WriteString("    - 情绪评分: 无数据\n")
				continue
			}
			if last := candles[len(candles)-1]; last.CloseTime > 0 {
				ts := time.UnixMilli(last.CloseTime)
				if ts.After(latestUpdate) {
					latestUpdate = ts
				}
			}

			var fp strings.Builder
			hasData := false
			fp.WriteString("iv=")
			fp.WriteString(iv)

			if cvd, ok := market.ComputeCVD(candles); ok {
				sb.WriteString(fmt.Sprintf("    - CVD: %s\n", cvd.Value.StringFixed(2)))
				sb.WriteString(fmt.Sprintf("      - CVD_MOM: %s\n", cvd.Momentum.StringFixed(2)))
				sb.WriteString(fmt.Sprintf("      - CVD_NORM: %s\n", cvd.Normalized.StringFixed(6)))
				sb.WriteString(fmt.Sprintf("      - CVD_DIVERGENCE: %s\n", cvd.Divergence))
				sb.WriteString(fmt.Sprintf("      - CVD_PEAKFLIP: %s\n", cvd.PeakFlip))
				hasData = true
				fp.WriteString("|cvd=")
				fp.WriteString(cvd.Value.StringFixed(2))
				fp.WriteString("|mom=")
				fp.WriteString(cvd.Momentum.StringFixed(2))
				fp.WriteString("|norm=")
				fp.WriteString(cvd.Normalized.StringFixed(6))
				fp.WriteString("|div=")
				fp.WriteString(cvd.Divergence)
				fp.WriteString("|flip=")
				fp.WriteString(cvd.PeakFlip)
			} else {
				sb.WriteString("    - CVD: 无数据\n")
			}

			if b.Sentiment != nil {
				if sent, ok := b.Sentiment.Calculate(ctx, sym, iv, candles); ok {
					sb.WriteString(fmt.Sprintf("    - 情绪评分: %d/100 (%s)\n", sent.Score, sent.Tag))
					sb.WriteString(fmt.Sprintf("      - OI情绪: %s\n", sent.Factors.OpenInterest.StringFixed(3)))
					sb.WriteString(fmt.Sprintf("      - Funding情绪: %s\n", sent.Factors.FundingRate.StringFixed(3)))
					sb.WriteString(fmt.Sprintf("      - 大户情绪: %s\n", sent.Factors.BigWhales.StringFixed(3)))
					sb.WriteString(fmt.Sprintf("      - 大户账户: %s\n", sent.Factors.BigAccounts.StringFixed(3)))
					sb.WriteString(fmt.Sprintf("      - 散户反向: %s\n", sent.Factors.RetailInverse.StringFixed(3)))
					sb.WriteString(fmt.Sprintf("      - 成交量情绪: %s\n", sent.Factors.VolumeEmotion.StringFixed(3)))
					hasData = true
					fp.WriteString("|sent=")
					fp.WriteString(fmt.Sprintf("%d", sent.Score))
					fp.WriteString("|oi_s=")
					fp.WriteString(sent.Factors.OpenInterest.StringFixed(3))
					fp.WriteString("|fund_s=")
					fp.WriteString(sent.Factors.FundingRate.StringFixed(3))
					fp.WriteString("|big=")
					fp.WriteString(sent.Factors.BigWhales.StringFixed(3))
					fp.WriteString("|big_acc=")
					fp.WriteString(sent.Factors.BigAccounts.StringFixed(3))
					fp.WriteString("|retail=")
					fp.WriteString(sent.Factors.RetailInverse.StringFixed(3))
					fp.WriteString("|vol=")
					fp.WriteString(sent.Factors.VolumeEmotion.StringFixed(3))
				} else {
					sb.WriteString("    - 情绪评分: 无数据\n")
				}
			}
			if hasData {
				fingerprintParts = append(fingerprintParts, fp.String())
			}
		}
	}
	sb.WriteString("请结合这些衍生品数据评估市场情绪和资金动向。\n")
	fingerprint := ""
	if len(fingerprintParts) > 0 {
		raw := strings.Join(fingerprintParts, "|")
		sum := sha256.Sum256([]byte(raw))
		fingerprint = hex.EncodeToString(sum[:])
	}
	return sb.String(), latestUpdate, fingerprint
}

func uniqueSymbols(ctxs []AnalysisContext) []string {
	set := make(map[string]struct{}, len(ctxs))
	for _, ac := range ctxs {
		sym := strings.ToUpper(strings.TrimSpace(ac.Symbol))
		if sym == "" {
			continue
		}
		set[sym] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for sym := range set {
		out = append(out, sym)
	}
	sort.Strings(out)
	return out
}

func groupIntervalsBySymbol(ctxs []AnalysisContext, rank map[string]int) map[string][]string {
	set := make(map[string]map[string]struct{}, len(ctxs))
	for _, ac := range ctxs {
		sym := strings.ToUpper(strings.TrimSpace(ac.Symbol))
		iv := strings.ToLower(strings.TrimSpace(ac.Interval))
		if sym == "" || iv == "" {
			continue
		}
		if _, ok := set[sym]; !ok {
			set[sym] = make(map[string]struct{})
		}
		set[sym][iv] = struct{}{}
	}
	out := make(map[string][]string, len(set))
	for sym, ivs := range set {
		items := make([]string, 0, len(ivs))
		for iv := range ivs {
			items = append(items, iv)
		}
		sort.Slice(items, func(i, j int) bool {
			ri := intervalRankValue(items[i], rank)
			rj := intervalRankValue(items[j], rank)
			if ri != rj {
				return ri < rj
			}
			return items[i] < items[j]
		})
		out[sym] = items
	}
	return out
}

func (b *DefaultPromptBuilder) renderKlineWindows(ctxs []AnalysisContext, directives map[string]ProfileDirective) string {
	ctxs = filterKlineContexts(ctxs, directives)
	windows, latestLine := collectKlineWindows(ctxs)
	if len(windows) == 0 {
		return ""
	}
	sortKlineWindows(windows, buildIntervalRank(b.Intervals))
	return renderKlineWindowsOutput(windows, latestLine)
}

func filterKlineContexts(ctxs []AnalysisContext, directives map[string]ProfileDirective) []AnalysisContext {
	if len(ctxs) == 0 || len(directives) == 0 {
		return ctxs
	}
	out := make([]AnalysisContext, 0, len(ctxs))
	for _, ac := range ctxs {
		dir, ok := lookupDirective(ac.Symbol, directives)
		if ok && !dir.IncludeKlines {
			continue
		}
		out = append(out, ac)
	}
	return out
}

func collectKlineWindows(ctxs []AnalysisContext) ([]klineWindow, string) {
	if len(ctxs) == 0 {
		return nil, ""
	}
	windows := make([]klineWindow, 0, len(ctxs))
	var latestLine string
	for _, ac := range ctxs {
		win, ok, line := buildKlineWindow(ac)
		if !ok {
			continue
		}
		if latestLine == "" && line != "" {
			latestLine = line
		}
		windows = append(windows, win)
	}
	return windows, latestLine
}

func buildKlineWindow(ac AnalysisContext) (klineWindow, bool, string) {
	csvData := strings.TrimSpace(ac.KlineCSV)
	bars, err := parseRecentCandles(ac.KlineJSON, priceWindowBars)
	if err != nil {
		logger.Debugf("kline snapshot 解析失败 %s %s: %v", ac.Symbol, ac.Interval, err)
		return klineWindow{}, false, ""
	}
	if len(bars) == 0 && csvData == "" {
		return klineWindow{}, false, ""
	}
	symbol := strings.ToUpper(strings.TrimSpace(ac.Symbol))
	interval := strings.TrimSpace(ac.Interval)
	if symbol == "" || interval == "" {
		return klineWindow{}, false, ""
	}
	win := klineWindow{
		Symbol:   symbol,
		Interval: interval,
		Horizon:  strings.TrimSpace(ac.ForecastHorizon),
		Trend:    ac.TrendReport,
		CSV:      csvData,
		Bars:     bars,
	}
	return win, true, buildLatestPriceLine(symbol, bars)
}

func buildLatestPriceLine(symbol string, bars []market.Candle) string {
	if len(bars) == 0 || strings.TrimSpace(symbol) == "" {
		return ""
	}
	latest := bars[len(bars)-1]
	return fmt.Sprintf("最新价格：%s 收=%.4f 时间=%s",
		strings.ToUpper(strings.TrimSpace(symbol)),
		latest.Close,
		time.UnixMilli(latest.CloseTime).UTC().Format(time.RFC3339))
}

func sortKlineWindows(windows []klineWindow, rank map[string]int) {
	if len(windows) < 2 {
		return
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
}

func renderKlineWindowsOutput(windows []klineWindow, latestLine string) string {
	var sb strings.Builder
	sb.WriteString("\n## Price Windows（最近 4 根，最新在前）\n")
	if latestLine != "" {
		sb.WriteString(latestLine + "\n")
	}
	for _, win := range windows {
		writeKlineWindow(&sb, win)
	}
	sb.WriteString("请结合这些时间窗口评估当前价格位置与动量。\n")
	return sb.String()
}

func writeKlineWindow(sb *strings.Builder, win klineWindow) {
	if sb == nil || win.Symbol == "" || win.Interval == "" {
		return
	}
	header := fmt.Sprintf("- %s %s", win.Symbol, win.Interval)
	if win.Horizon != "" {
		header += fmt.Sprintf(" (%s)", win.Horizon)
	}
	sb.WriteString(header + "\n")
	for idx := len(win.Bars) - 1; idx >= 0; idx-- {
		bar := win.Bars[idx]
		fmt.Fprintf(sb, "    [%s, o=%s, h=%s, l=%s, c=%s, v=%.2f]\n",
			bar.TimeString(),
			formatutil.Float(bar.Open, 4),
			formatutil.Float(bar.High, 4),
			formatutil.Float(bar.Low, 4),
			formatutil.Float(bar.Close, 4),
			bar.Volume,
		)
	}
	if len(win.Bars) > 0 {
		if summary := market.Candles(win.Bars).Snapshot(win.Interval, win.Trend); summary != "" {
			sb.WriteString("  Snapshot: " + summary + "\n")
		}
		sb.WriteString("\n")
	}
}

func logStructuredBlocksDebug(debug bool, ctxs []AnalysisContext) {
	if !debug || len(ctxs) == 0 {
		return
	}
	limit := 4
	if len(ctxs) < limit {
		limit = len(ctxs)
	}
	var b strings.Builder
	b.WriteString("--- Structured Debug ---\n")
	for i := 0; i < limit; i++ {
		ac := ctxs[i]
		b.WriteString(fmt.Sprintf("* %s %s (%s)\n", strings.ToUpper(ac.Symbol), ac.Interval, ac.ForecastHorizon))
		if ac.PatternReport != "" {
			b.WriteString("  形态: " + textutil.Truncate(ac.PatternReport, 240) + "\n")
		}
		if ac.TrendReport != "" {
			b.WriteString("  趋势: " + textutil.Truncate(ac.TrendReport, 240) + "\n")
		}
		if ac.ImageNote != "" {
			b.WriteString("  图示: " + textutil.Truncate(ac.ImageNote, 160) + "\n")
		}
		if ac.IndicatorJSON != "" {
			b.WriteString("  指标JSON:\n")
			b.WriteString(textutil.Truncate(jsonutil.Pretty(ac.IndicatorJSON), 600))
			b.WriteString("\n")
		}
		if ac.KlineJSON != "" {
			b.WriteString("  RawKline:\n")
			b.WriteString(textutil.Truncate(ac.KlineJSON, 600))
			b.WriteString("\n")
		}
	}
	b.WriteString("--- End Structured Debug ---")
	logger.Debugf("%s", b.String())
}
