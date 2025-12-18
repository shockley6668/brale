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

// CloseFreqtradePosition closes a position by sending an exit signal to the Trader actor.
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
	m.trader.Send(trader.EventEnvelope{
		ID:        managerEventID("", "signal-exit"),
		Type:      trader.EvtSignalExit,
		Payload:   data,
		CreatedAt: time.Now(),
		Symbol:    strings.ToUpper(strings.TrimSpace(symbol)),
	})
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

// ManualOpenPosition provides an admin UI shortcut to open a trade and materialize its exit plan instances.
func (m *Manager) ManualOpenPosition(ctx context.Context, req exchange.ManualOpenRequest) error {
	if m == nil || m.executor == nil {
		return fmt.Errorf("executor not initialized")
	}
	symbol := strings.ToUpper(strings.TrimSpace(req.Symbol))
	if symbol == "" {
		return fmt.Errorf("symbol 必填")
	}
	side := strings.ToLower(strings.TrimSpace(req.Side))
	if side != "long" && side != "short" {
		return fmt.Errorf("side 需为 long 或 short")
	}
	if req.PositionSizeUSD <= 0 {
		return fmt.Errorf("position_size_usd 需 >0")
	}
	if req.Leverage <= 0 {
		return fmt.Errorf("leverage 需 >0")
	}
	entryPrice := req.EntryPrice
	if entryPrice <= 0 {
		return fmt.Errorf("entry_price 需 >0")
	}
	comboKey := strings.ToLower(strings.TrimSpace(req.ExitCombo))
	if comboKey == "" {
		return fmt.Errorf("exit_combo 必填")
	}

	planSpec, err := buildManualComboPlanSpec(req, entryPrice, side, comboKey)
	if err != nil {
		return err
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

	reg := exit.NewHandlerRegistry()
	exithandlers.RegisterCoreHandlers(reg)
	comboHandler, _ := reg.Handler("combo_group")
	if comboHandler == nil {
		return fmt.Errorf("exit plan handler 未注册")
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
		return err
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
	type tier struct {
		Target float64
		Ratio  float64
	}
	normalize := func(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
	parts := strings.Split(normalize(comboKey), "__")
	if len(parts) < 2 {
		return nil, fmt.Errorf("exit_combo 格式错误: %s", comboKey)
	}
	hasTP := false
	hasSL := false
	seen := map[string]bool{}
	children := make([]any, 0, len(parts))

	buildTierParams := func(tiers []tier) (map[string]any, error) {
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

	tpTiers := []tier{}
	if req.Tier1Target > 0 {
		tpTiers = append(tpTiers, tier{Target: req.Tier1Target, Ratio: req.Tier1Ratio})
	}
	if req.Tier2Target > 0 {
		tpTiers = append(tpTiers, tier{Target: req.Tier2Target, Ratio: req.Tier2Ratio})
	}
	if req.Tier3Target > 0 {
		tpTiers = append(tpTiers, tier{Target: req.Tier3Target, Ratio: req.Tier3Ratio})
	}

	slTiers := []tier{}
	if req.SLTier1Target > 0 {
		slTiers = append(slTiers, tier{Target: req.SLTier1Target, Ratio: req.SLTier1Ratio})
	}
	if req.SLTier2Target > 0 {
		slTiers = append(slTiers, tier{Target: req.SLTier2Target, Ratio: req.SLTier2Ratio})
	}
	if req.SLTier3Target > 0 {
		slTiers = append(slTiers, tier{Target: req.SLTier3Target, Ratio: req.SLTier3Ratio})
	}

	for _, raw := range parts {
		comp := normalize(raw)
		if comp == "" {
			continue
		}
		if seen[comp] {
			return nil, fmt.Errorf("exit_combo component 重复: %s", comp)
		}
		seen[comp] = true
		switch comp {
		case "tp_single":
			hasTP = true
			if req.TakeProfit <= 0 {
				return nil, fmt.Errorf("tp_single 需提供 take_profit")
			}
			params, err := buildTierParams([]tier{{Target: req.TakeProfit, Ratio: 1}})
			if err != nil {
				return nil, err
			}
			children = append(children, map[string]any{
				"component": "tp_single",
				"handler":   "tier_take_profit",
				"params":    params,
			})
		case "tp_tiers":
			hasTP = true
			if len(tpTiers) == 0 {
				return nil, fmt.Errorf("tp_tiers 需提供 tier1/2/3 目标价")
			}
			params, err := buildTierParams(tpTiers)
			if err != nil {
				return nil, err
			}
			children = append(children, map[string]any{
				"component": "tp_tiers",
				"handler":   "tier_take_profit",
				"params":    params,
			})
		case "sl_single":
			hasSL = true
			if req.StopLoss <= 0 {
				return nil, fmt.Errorf("sl_single 需提供 stop_loss")
			}
			params, err := buildTierParams([]tier{{Target: req.StopLoss, Ratio: 1}})
			if err != nil {
				return nil, err
			}
			children = append(children, map[string]any{
				"component": "sl_single",
				"handler":   "tier_stop_loss",
				"params":    params,
			})
		case "sl_tiers":
			hasSL = true
			if len(slTiers) == 0 {
				return nil, fmt.Errorf("sl_tiers 需提供 sl_tier1/2/3 目标价（不再自动生成）")
			}
			params, err := buildTierParams(slTiers)
			if err != nil {
				return nil, err
			}
			children = append(children, map[string]any{
				"component": "sl_tiers",
				"handler":   "tier_stop_loss",
				"params":    params,
			})
		case "tp_atr", "sl_atr":
			return nil, fmt.Errorf("手动开仓暂不支持 ATR 组件: %s", comp)
		default:
			return nil, fmt.Errorf("未知 exit_combo component: %s", comp)
		}
	}
	if !hasTP || !hasSL {
		return nil, fmt.Errorf("exit_combo 必须同时包含止盈(tp_*)与止损(sl_*)组件")
	}
	return map[string]any{"children": children}, nil
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
