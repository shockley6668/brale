package freqtrade

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"brale/internal/decision"
	"brale/internal/gateway/database"
	"brale/internal/gateway/exchange"
	"brale/internal/logger"
	"brale/internal/strategy/exit"
	exithandlers "brale/internal/strategy/exit/handlers"
	"brale/internal/trader"
)

func (m *Manager) CloseFreqtradePosition(ctx context.Context, tradeID int, symbol, side string, closeRatio float64) error {
	if m.trader == nil {
		return fmt.Errorf("trader not initialized")
	}

	if tradeID > 0 {
		if err := m.validateTradeForClose(ctx, tradeID, symbol); err != nil {
			return err
		}
	}

	payload := trader.SignalExitPayload{
		TradeID:        tradeID,
		Symbol:         symbol,
		Side:           side,
		CloseRatio:     closeRatio,
		IsInitialRatio: true,
	}

	data, _ := json.Marshal(payload)
	if err := m.trader.Send(trader.EventEnvelope{
		ID:        managerEventID("", "signal-exit"),
		Type:      trader.EvtSignalExit,
		Payload:   data,
		CreatedAt: time.Now(),
		Symbol:    strings.ToUpper(strings.TrimSpace(symbol)),
	}); err != nil {
		return err
	}
	return nil
}

func (m *Manager) validateTradeForClose(ctx context.Context, tradeID int, symbol string) error {
	if norm := freqtradePairToSymbol(symbol); norm != "" {
		symbol = norm
	}
	activeID, exists := m.TradeIDBySymbol(symbol)
	if exists {
		if activeID != tradeID {
			return fmt.Errorf("CloseFreqtradePosition: trade mismatch query=%d active=%d for %s", tradeID, activeID, symbol)
		}
		return nil
	}

	if m.posRepo != nil {
		rec, ok, err := m.posRepo.GetPosition(ctx, tradeID)
		if err != nil {
			logger.Warnf("CloseFreqtradePosition: DB query failed for trade %d: %v", tradeID, err)
		}
		if ok && rec.Status != database.LiveOrderStatusClosed && rec.Status != database.LiveOrderStatusClosingFull {
			logger.Warnf("CloseFreqtradePosition: trade %d not in memory but found in DB (status=%d), proceeding", tradeID, rec.Status)
			return nil
		}
	}

	return fmt.Errorf("CloseFreqtradePosition: trade %d not active for %s", tradeID, symbol)
}

func (m *Manager) ManualOpenPosition(ctx context.Context, req exchange.ManualOpenRequest) error {
	if m == nil || m.executor == nil {
		return fmt.Errorf("executor not initialized")
	}
	symbol, side, entryPrice, comboKey, err := validateManualOpenRequest(req)
	if err != nil {
		return err
	}

	// Build guard decision to reuse existing validation on stop distance.
	planSpec, err := buildManualComboPlanSpec(req, entryPrice, side, comboKey)
	if err != nil {
		return err
	}

	// Validate exit plan structure before触发开仓，避免开仓成功但策略保存失败
	reg := exit.NewHandlerRegistry()
	exithandlers.RegisterCoreHandlers(reg)
	comboHandler, _ := reg.Handler("combo_group")
	if comboHandler == nil {
		return fmt.Errorf("exit plan handler 未注册")
	}
	if _, err := comboHandler.Instantiate(ctx, exit.InstantiateArgs{
		TradeID:       -1, // dry-run
		PlanID:        "plan_combo_main",
		PlanVersion:   1,
		PlanSpec:      planSpec,
		EntryPrice:    entryPrice,
		Side:          side,
		Symbol:        symbol,
		DecisionTrace: "manual-open:dry-run",
	}); err != nil {
		return fmt.Errorf("exit plan 校验失败: %w", err)
	}

	guardDecision := decision.Decision{
		Symbol: symbol,
		Action: "open_" + side,
		ExitPlan: &decision.ExitPlanSpec{
			ID:     "plan_combo_main",
			Params: planSpec,
		},
	}
	if err := m.validateInitialStopDistance(guardDecision, side, entryPrice); err != nil {
		return err
	}

	entryTag := buildManualEntryTag(req)

	openReq := exchange.OpenRequest{
		Symbol:    symbol,
		Side:      side,
		OrderType: "limit",
		Price:     entryPrice,
		Amount:    req.PositionSizeUSD,
		Tag:       entryTag,
	}
	if req.Leverage > 0 {
		openReq.Leverage = float64(req.Leverage)
	}

	result, err := m.executor.OpenPosition(ctx, openReq)
	if err != nil {
		return err
	}
	tradeID, _ := strconv.Atoi(strings.TrimSpace(result.PositionID))
	if tradeID <= 0 {
		return fmt.Errorf("freqtrade 未返回 trade_id")
	}

	planID := "plan_combo_main"
	decisionTrace := fmt.Sprintf("manual-open:%d", tradeID)
	instances, err := comboHandler.Instantiate(ctx, exit.InstantiateArgs{
		TradeID:       tradeID,
		PlanID:        planID,
		PlanVersion:   1,
		PlanSpec:      planSpec,
		EntryPrice:    entryPrice,
		Side:          side,
		Symbol:        symbol,
		DecisionTrace: decisionTrace,
	})
	if err != nil {
		_ = m.executor.ClosePosition(ctx, exchange.CloseRequest{
			PositionID: strconv.Itoa(tradeID),
			Symbol:     symbol,
			Side:       side,
			Amount:     0, // full close
		})
		return fmt.Errorf("exit plan 初始化失败，已请求平仓: %w", err)
	}
	if m.posStore == nil {
		return fmt.Errorf("posStore not initialized")
	}
	recs := make([]database.StrategyInstanceRecord, 0, len(instances))
	for _, inst := range instances {
		recs = append(recs, inst.Record)
	}
	if err := m.posStore.InsertStrategyInstances(ctx, recs); err != nil {
		return err
	}
	m.logPlanInit(ctx, tradeID, planID, decisionTrace, "manual_open")
	_ = m.SyncStrategyPlans(ctx, tradeID, buildPlanSnapshots(recs))
	if m.planUpdateHook != nil {
		m.planUpdateHook.NotifyPlanUpdated(context.Background(), tradeID)
	}
	return nil
}

func buildManualComboPlanSpec(req exchange.ManualOpenRequest, entryPrice float64, side string, comboKey string) (map[string]any, error) {
	components, err := parseComboComponents(comboKey)
	if err != nil {
		return nil, err
	}

	tpTiers := manualTiers(req.Tier1Target, req.Tier1Ratio, req.Tier2Target, req.Tier2Ratio, req.Tier3Target, req.Tier3Ratio)
	slTiers := manualTiers(req.SLTier1Target, req.SLTier1Ratio, req.SLTier2Target, req.SLTier2Ratio, req.SLTier3Target, req.SLTier3Ratio)

	hasTP := false
	hasSL := false
	children := make([]any, 0, len(components))
	for _, comp := range components {
		child, kind, err := buildManualComboChild(comp, req, tpTiers, slTiers)
		if err != nil {
			return nil, err
		}
		switch kind {
		case "tp":
			hasTP = true
		case "sl":
			hasSL = true
		}
		children = append(children, child)
	}
	if !hasTP || !hasSL {
		return nil, fmt.Errorf("exit_combo 必须同时包含止盈(tp_*)与止损(sl_*)组件")
	}
	return map[string]any{"children": children}, nil
}

func validateManualOpenRequest(req exchange.ManualOpenRequest) (symbol, side string, entryPrice float64, comboKey string, err error) {
	symbol = strings.ToUpper(strings.TrimSpace(req.Symbol))
	if symbol == "" {
		err = fmt.Errorf("symbol 必填")
		return
	}
	side = strings.ToLower(strings.TrimSpace(req.Side))
	if side != "long" && side != "short" {
		err = fmt.Errorf("side 需为 long 或 short")
		return
	}
	if req.PositionSizeUSD <= 0 {
		err = fmt.Errorf("position_size_usd 需 >0")
		return
	}
	if req.Leverage <= 0 {
		err = fmt.Errorf("leverage 需 >0")
		return
	}
	entryPrice = req.EntryPrice
	if entryPrice <= 0 {
		err = fmt.Errorf("entry_price 需 >0")
		return
	}
	comboKey = strings.ToLower(strings.TrimSpace(req.ExitCombo))
	if comboKey == "" {
		err = fmt.Errorf("exit_combo 必填")
		return
	}
	return
}

// buildManualEntryTag composes a readable tag like:
// "manual | tp_tiers__sl_single | reason text", truncated to 120 chars.
func buildManualEntryTag(req exchange.ManualOpenRequest) string {
	tagParts := []string{"manual"}
	if strings.TrimSpace(req.ExitCombo) != "" {
		tagParts = append(tagParts, strings.TrimSpace(req.ExitCombo))
	}
	if strings.TrimSpace(req.Reason) != "" {
		tagParts = append(tagParts, strings.TrimSpace(req.Reason))
	}
	entryTag := strings.Join(tagParts, " | ")
	if len(entryTag) > 120 {
		entryTag = entryTag[:120]
	}
	return entryTag
}

type manualTier struct {
	Target float64
	Ratio  float64
}

func manualTiers(t1, r1, t2, r2, t3, r3 float64) []manualTier {
	var out []manualTier
	if t1 > 0 {
		out = append(out, manualTier{Target: t1, Ratio: r1})
	}
	if t2 > 0 {
		out = append(out, manualTier{Target: t2, Ratio: r2})
	}
	if t3 > 0 {
		out = append(out, manualTier{Target: t3, Ratio: r3})
	}
	return out
}

func parseComboComponents(comboKey string) ([]string, error) {
	normalize := func(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
	parts := strings.Split(normalize(comboKey), "__")
	if len(parts) < 2 {
		return nil, fmt.Errorf("exit_combo 格式错误: %s", comboKey)
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(parts))
	for _, raw := range parts {
		comp := normalize(raw)
		if comp == "" {
			continue
		}
		if seen[comp] {
			return nil, fmt.Errorf("exit_combo component 重复: %s", comp)
		}
		seen[comp] = true
		out = append(out, comp)
	}
	if len(out) < 2 {
		return nil, fmt.Errorf("exit_combo 格式错误: %s", comboKey)
	}
	return out, nil
}

func buildManualComboChild(comp string, req exchange.ManualOpenRequest, tpTiers, slTiers []manualTier) (map[string]any, string, error) {
	switch comp {
	case "tp_single":
		if req.TakeProfit <= 0 {
			return nil, "", fmt.Errorf("tp_single 需提供 take_profit")
		}
		params, err := buildManualTierParams([]manualTier{{Target: req.TakeProfit, Ratio: 1}})
		if err != nil {
			return nil, "", err
		}
		return map[string]any{"component": "tp_single", "handler": "tier_take_profit", "params": params}, "tp", nil
	case "tp_tiers":
		if len(tpTiers) == 0 {
			return nil, "", fmt.Errorf("tp_tiers 需提供 tier1/2/3 目标价")
		}
		params, err := buildManualTierParams(tpTiers)
		if err != nil {
			return nil, "", err
		}
		return map[string]any{"component": "tp_tiers", "handler": "tier_take_profit", "params": params}, "tp", nil
	case "sl_single":
		if req.StopLoss <= 0 {
			return nil, "", fmt.Errorf("sl_single 需提供 stop_loss")
		}
		params, err := buildManualTierParams([]manualTier{{Target: req.StopLoss, Ratio: 1}})
		if err != nil {
			return nil, "", err
		}
		return map[string]any{"component": "sl_single", "handler": "tier_stop_loss", "params": params}, "sl", nil
	case "sl_tiers":
		if len(slTiers) == 0 {
			return nil, "", fmt.Errorf("sl_tiers 需提供 sl_tier1/2/3 目标价（不再自动生成）")
		}
		params, err := buildManualTierParams(slTiers)
		if err != nil {
			return nil, "", err
		}
		return map[string]any{"component": "sl_tiers", "handler": "tier_stop_loss", "params": params}, "sl", nil
	case "tp_atr", "sl_atr":
		return nil, "", fmt.Errorf("手动开仓暂不支持 ATR 组件: %s", comp)
	default:
		return nil, "", fmt.Errorf("未知 exit_combo component: %s", comp)
	}
}

func buildManualTierParams(tiers []manualTier) (map[string]any, error) {
	if len(tiers) == 0 {
		return nil, fmt.Errorf("tiers 不能为空")
	}
	if len(tiers) > 3 {
		tiers = tiers[:3]
	}
	sum := 0.0
	raw := make([]any, 0, len(tiers))
	for _, t := range tiers {
		if t.Target <= 0 {
			return nil, fmt.Errorf("tier target_price 必须 >0")
		}
		if t.Ratio <= 0 || t.Ratio > 1 {
			return nil, fmt.Errorf("tier ratio 需位于 (0,1]")
		}
		sum += t.Ratio
		raw = append(raw, map[string]any{
			"target_price": t.Target,
			"ratio":        t.Ratio,
		})
	}
	if len(tiers) == 1 {
		raw[0].(map[string]any)["ratio"] = 1.0
		sum = 1.0
	}
	if math.Abs(sum-1) > 1e-6 {
		return nil, fmt.Errorf("tiers 比例合计需为 1，当前=%.4f", sum)
	}
	return map[string]any{"tiers": raw}, nil
}

func (m *Manager) currentPositionAmount(symbol string) float64 {
	if m == nil || m.trader == nil {
		return 0
	}
	snap := m.trader.Snapshot()
	if snap == nil || snap.Positions == nil {
		return 0
	}
	key := freqtradePairToSymbol(symbol)
	if key == "" {
		key = strings.ToUpper(strings.TrimSpace(symbol))
	}
	pos, ok := snap.Positions[key]
	if !ok || pos == nil {
		return 0
	}
	return pos.Amount
}

func (m *Manager) deriveCloseBreakdown(symbol string, executed float64) (float64, float64) {
	current := m.currentPositionAmount(symbol)
	if current <= 0 {
		return executed, 0
	}
	closed := executed
	if closed <= 0 || closed > current {
		closed = current
	}
	remaining := current - closed
	if remaining < 0 {
		remaining = 0
	}
	return closed, remaining
}
