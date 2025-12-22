package freqtrade

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"brale/internal/decision"
	"brale/internal/gateway/database"
	"brale/internal/gateway/exchange"
	"brale/internal/logger"
	"brale/internal/strategy/exit"
	"brale/internal/trader"
)

type strategyChangeLogWriter interface {
	InsertStrategyChangeLog(ctx context.Context, rec database.StrategyChangeLogRecord) error
}

func (m *Manager) logPlanInit(ctx context.Context, tradeID int, planID, traceID, source string) {
	if m == nil || m.posStore == nil || tradeID <= 0 {
		return
	}
	planID = strings.TrimSpace(planID)
	if planID == "" {
		return
	}
	writer, ok := m.posStore.(strategyChangeLogWriter)
	if !ok {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	rec := database.StrategyChangeLogRecord{
		TradeID:         tradeID,
		InstanceID:      0,
		PlanID:          planID,
		PlanComponent:   "",
		ChangedField:    "plan_init",
		OldValue:        "",
		NewValue:        "",
		TriggerSource:   strings.TrimSpace(source),
		Reason:          fmt.Sprintf("初始化 Exit Plan: %s", planID),
		DecisionTraceID: strings.TrimSpace(traceID),
		CreatedAt:       time.Now(),
	}
	if err := writer.InsertStrategyChangeLog(ctx, rec); err != nil {
		logger.Warnf("freqtrade: 写 strategy_change_log(init) 失败 trade=%d plan=%s err=%v", tradeID, planID, err)
	}
}

func (m *Manager) PublishPlanStateUpdate(ctx context.Context, payload exchange.PlanStateUpdatePayload) error {
	if m.trader == nil {
		return fmt.Errorf("trader not initialized")
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	m.trader.Send(trader.EventEnvelope{
		ID:        managerEventID("", "plan-state"),
		Type:      trader.EvtPlanStateUpdate,
		Payload:   data,
		CreatedAt: time.Now(),
		TradeID:   payload.TradeID,
	})
	return nil
}

func (m *Manager) CacheDecision(key string, d decision.Decision) string {
	m.cacheOpenExitPlan(key, d)
	return key
}

func (m *Manager) ListStrategyInstances(ctx context.Context, tradeID int) ([]database.StrategyInstanceRecord, error) {
	if m == nil || m.posStore == nil {
		return nil, fmt.Errorf("posStore not initialized")
	}
	return m.posStore.ListStrategyInstances(ctx, tradeID)
}

func (m *Manager) SyncStrategyPlans(ctx context.Context, tradeID int, plans any) error {
	if m.trader == nil {
		return nil
	}
	planSnapshots, ok := plans.([]exit.StrategyPlanSnapshot)
	if !ok {
		return fmt.Errorf("invalid plans type, expected []exit.StrategyPlanSnapshot")
	}

	payload := trader.SyncPlansPayload{
		TradeID: tradeID,
		Version: time.Now().UnixNano(),
		Plans:   planSnapshots,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	m.trader.Send(trader.EventEnvelope{
		ID:        managerEventID("", "sync-plans"),
		Type:      trader.EvtSyncPlans,
		Payload:   data,
		CreatedAt: time.Now(),
		TradeID:   tradeID,
	})
	return nil
}

func (m *Manager) SetPlanUpdateHook(hook exchange.PlanUpdateHook) {
	m.planUpdateHook = hook
}

func (m *Manager) NotifyPlanUpdated(ctx context.Context, tradeID int) {
	recs, err := m.posStore.ListStrategyInstances(ctx, tradeID)
	if err != nil {
		logger.Warnf("NotifyPlanUpdated: failed to list plans for %d: %v", tradeID, err)
		return
	}
	snapshots := buildPlanSnapshots(recs)
	_ = m.SyncStrategyPlans(ctx, tradeID, snapshots)
}

func buildPlanSnapshots(recs []database.StrategyInstanceRecord) []exit.StrategyPlanSnapshot {
	if len(recs) == 0 {
		return nil
	}
	out := make([]exit.StrategyPlanSnapshot, 0, len(recs))
	for _, rec := range recs {
		out = append(out, exit.StrategyPlanSnapshot{
			PlanID:          rec.PlanID,
			PlanComponent:   rec.PlanComponent,
			PlanVersion:     rec.PlanVersion,
			StatusLabel:     exit.StatusLabel(rec.Status),
			StatusCode:      int(rec.Status),
			ParamsJSON:      rec.ParamsJSON,
			StateJSON:       rec.StateJSON,
			DecisionTraceID: rec.DecisionTraceID,
			CreatedAt:       rec.CreatedAt.Unix(),
			UpdatedAt:       rec.UpdatedAt.Unix(),
		})
	}
	return out
}
