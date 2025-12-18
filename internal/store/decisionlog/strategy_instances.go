package decisionlog

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	storemodel "brale/internal/store/model"
)

// StrategyStatus 描述 plan 执行状态。
type StrategyStatus = storemodel.StrategyStatus

const (
	// Legacy naming (Waiting/Pending/Paused) mapped onto shared statuses.
	StrategyStatusWaiting StrategyStatus = storemodel.StrategyStatusPending
	StrategyStatusPending StrategyStatus = storemodel.StrategyStatusActive
	StrategyStatusDone    StrategyStatus = storemodel.StrategyStatusDone
	StrategyStatusPaused  StrategyStatus = storemodel.StrategyStatusFailed

	// Direct aliases for shared enums.
	StrategyStatusActive StrategyStatus = storemodel.StrategyStatusActive
	StrategyStatusFailed StrategyStatus = storemodel.StrategyStatusFailed
)

// StrategyInstanceRecord 表示 strategy_instances 的一行。
type StrategyInstanceRecord struct {
	ID              int64
	TradeID         int
	PlanID          string
	PlanComponent   string
	PlanVersion     int
	ParamsJSON      string
	StateJSON       string
	Status          StrategyStatus
	DecisionTraceID string
	LastEvalAt      *time.Time
	NextEvalAfter   *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// StrategyChangeLogRecord 记录 plan 调整日志。
type StrategyChangeLogRecord struct {
	TradeID         int
	InstanceID      int64
	PlanID          string
	PlanComponent   string
	ChangedField    string
	OldValue        string
	NewValue        string
	TriggerSource   string
	Reason          string
	DecisionTraceID string
	CreatedAt       time.Time
}

func (s *DecisionLogStore) InsertStrategyInstances(ctx context.Context, recs []StrategyInstanceRecord) error {
	if s == nil || len(recs) == 0 {
		return nil
	}
	s.mu.Lock()
	db := s.db
	s.mu.Unlock()
	if db == nil {
		return fmt.Errorf("decision log store 未初始化")
	}
	now := time.Now()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return err
	}
	stmt := `INSERT INTO strategy_instances
		(trade_id, plan_id, plan_component, plan_version, params_json, state_json, status, decision_trace_id, last_eval_at, next_eval_after, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(trade_id, plan_id, plan_component)
		DO UPDATE SET
			params_json=excluded.params_json,
			plan_version=excluded.plan_version,
			state_json=COALESCE(excluded.state_json, strategy_instances.state_json),
			status=excluded.status,
			decision_trace_id=COALESCE(excluded.decision_trace_id, strategy_instances.decision_trace_id),
			last_eval_at=COALESCE(excluded.last_eval_at, strategy_instances.last_eval_at),
			next_eval_after=COALESCE(excluded.next_eval_after, strategy_instances.next_eval_after),
			updated_at=excluded.updated_at`
	for _, rec := range recs {
		rec.normalize(now)
		if _, err := tx.ExecContext(ctx, stmt,
			rec.TradeID,
			rec.PlanID,
			rec.PlanComponent,
			rec.PlanVersion,
			rec.ParamsJSON,
			rec.StateJSON,
			int(rec.Status),
			nullString(rec.DecisionTraceID),
			nullUnix(rec.LastEvalAt),
			nullUnix(rec.NextEvalAfter),
			rec.CreatedAt.Unix(),
			rec.UpdatedAt.Unix(),
		); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (s *DecisionLogStore) InsertStrategyChangeLog(ctx context.Context, rec StrategyChangeLogRecord) error {
	if s == nil {
		return fmt.Errorf("decision log store 未初始化")
	}
	s.mu.Lock()
	db := s.db
	s.mu.Unlock()
	if db == nil {
		return fmt.Errorf("decision log store 未初始化")
	}
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now()
	}
	_, err := db.ExecContext(ctx, `INSERT INTO strategy_change_log
		(trade_id, instance_id, plan_id, plan_component, changed_field, old_value, new_value, trigger_source, reason, decision_trace_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.TradeID,
		rec.InstanceID,
		rec.PlanID,
		rec.PlanComponent,
		rec.ChangedField,
		rec.OldValue,
		rec.NewValue,
		rec.TriggerSource,
		rec.Reason,
		rec.DecisionTraceID,
		rec.CreatedAt.Unix(),
	)
	return err
}

// ListStrategyChangeLogs 返回 plan 的变更记录。
func (s *DecisionLogStore) ListStrategyChangeLogs(ctx context.Context, tradeID int, limit int) ([]StrategyChangeLogRecord, error) {
	if s == nil {
		return nil, fmt.Errorf("decision log store 未初始化")
	}
	if tradeID <= 0 {
		return nil, fmt.Errorf("trade_id 必填")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	s.mu.Lock()
	db := s.db
	s.mu.Unlock()
	if db == nil {
		return nil, fmt.Errorf("decision log store 未初始化")
	}
	rows, err := db.QueryContext(ctx, `SELECT trade_id, instance_id, plan_id, plan_component, changed_field, old_value, new_value, trigger_source, reason, decision_trace_id, created_at
		FROM strategy_change_log WHERE trade_id = ? ORDER BY created_at DESC LIMIT ?`, tradeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StrategyChangeLogRecord
	for rows.Next() {
		var rec StrategyChangeLogRecord
		var created int64
		if err := rows.Scan(&rec.TradeID, &rec.InstanceID, &rec.PlanID, &rec.PlanComponent, &rec.ChangedField, &rec.OldValue, &rec.NewValue, &rec.TriggerSource, &rec.Reason, &rec.DecisionTraceID, &created); err != nil {
			return nil, err
		}
		rec.CreatedAt = time.Unix(created, 0)
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (s *DecisionLogStore) UpdateStrategyInstanceState(ctx context.Context, tradeID int, planID, planComponent, stateJSON string, status StrategyStatus) error {
	if s == nil {
		return fmt.Errorf("decision log store 未初始化")
	}
	s.mu.Lock()
	db := s.db
	s.mu.Unlock()
	if db == nil {
		return fmt.Errorf("decision log store 未初始化")
	}
	planID = strings.TrimSpace(planID)
	planComponent = strings.TrimSpace(planComponent)
	if tradeID <= 0 || planID == "" {
		return fmt.Errorf("plan 更新参数缺失 trade_id=%d plan_id=%s", tradeID, planID)
	}
	if strings.TrimSpace(stateJSON) == "" {
		stateJSON = "{}"
	}
	now := time.Now().Unix()
	res, err := db.ExecContext(ctx, `UPDATE strategy_instances
		SET state_json=?, status=?, updated_at=?
		WHERE trade_id=? AND plan_id=? AND plan_component=?`,
		stateJSON,
		int(status),
		now,
		tradeID,
		planID,
		planComponent,
	)
	if err != nil {
		return err
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *DecisionLogStore) ListStrategyInstances(ctx context.Context, tradeID int) ([]StrategyInstanceRecord, error) {
	if s == nil {
		return nil, fmt.Errorf("decision log store 未初始化")
	}
	s.mu.Lock()
	db := s.db
	s.mu.Unlock()
	if db == nil {
		return nil, fmt.Errorf("decision log store 未初始化")
	}
	if tradeID <= 0 {
		return nil, fmt.Errorf("trade_id 必填")
	}
	rows, err := db.QueryContext(ctx, `SELECT id, plan_id, plan_component, plan_version, params_json, state_json, status,
		decision_trace_id, last_eval_at, next_eval_after, created_at, updated_at
		FROM strategy_instances WHERE trade_id = ?`, tradeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StrategyInstanceRecord
	for rows.Next() {
		var rec StrategyInstanceRecord
		var lastEval, nextEval sql.NullInt64
		var created, updated int64
		var state, traceID sql.NullString
		if err := rows.Scan(
			&rec.ID,
			&rec.PlanID,
			&rec.PlanComponent,
			&rec.PlanVersion,
			&rec.ParamsJSON,
			&state,
			&rec.Status,
			&traceID,
			&lastEval,
			&nextEval,
			&created,
			&updated,
		); err != nil {
			return nil, err
		}
		if state.Valid {
			rec.StateJSON = state.String
		}
		if traceID.Valid {
			rec.DecisionTraceID = traceID.String
		}
		if lastEval.Valid {
			ts := time.Unix(lastEval.Int64, 0)
			rec.LastEvalAt = &ts
		}
		if nextEval.Valid {
			ts := time.Unix(nextEval.Int64, 0)
			rec.NextEvalAfter = &ts
		}
		rec.CreatedAt = time.Unix(created, 0)
		rec.UpdatedAt = time.Unix(updated, 0)
		rec.TradeID = tradeID
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (r *StrategyInstanceRecord) normalize(now time.Time) {
	if now.IsZero() {
		now = time.Now()
	}
	if r.TradeID <= 0 {
		panic("strategy instance requires trade_id")
	}
	r.PlanID = strings.TrimSpace(r.PlanID)
	if r.PlanComponent == "" {
		r.PlanComponent = ""
	}
	if r.PlanVersion <= 0 {
		r.PlanVersion = 1
	}
	if strings.TrimSpace(r.ParamsJSON) == "" {
		r.ParamsJSON = "{}"
	}
	if strings.TrimSpace(r.StateJSON) == "" {
		r.StateJSON = "{}"
	}
	if r.Status == 0 && r.Status != StrategyStatusWaiting {
		r.Status = StrategyStatusWaiting
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = now
	}
	if r.UpdatedAt.IsZero() {
		r.UpdatedAt = now
	}
}

func nullUnix(t *time.Time) interface{} {
	if t == nil || t.IsZero() {
		return nil
	}
	return t.Unix()
}

func nullString(val string) interface{} {
	val = strings.TrimSpace(val)
	if val == "" {
		return nil
	}
	return val
}

// EncodeParams 将参数 map 编码为 JSON 字符串。
func EncodeParams(params map[string]any) string {
	if len(params) == 0 {
		return "{}"
	}
	buf, err := json.Marshal(params)
	if err != nil {
		return "{}"
	}
	return string(buf)
}

// ActiveStrategyTrades 返回存在未完成策略实例的 trade_id 列表。
func (s *DecisionLogStore) ActiveStrategyTrades(ctx context.Context) ([]int, error) {
	if s == nil {
		return nil, fmt.Errorf("decision log store 未初始化")
	}
	s.mu.Lock()
	db := s.db
	s.mu.Unlock()
	if db == nil {
		return nil, fmt.Errorf("decision log store 未初始化")
	}
	rows, err := db.QueryContext(ctx, `SELECT DISTINCT trade_id FROM strategy_instances WHERE status != ?`, int(StrategyStatusDone))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if id > 0 {
			ids = append(ids, id)
		}
	}
	return ids, rows.Err()
}
