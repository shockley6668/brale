package gormstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"brale/internal/gateway/database"
	storemodel "brale/internal/store/model"

	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

type strategyInstanceModel = storemodel.StrategyInstanceModel
type liveOrderModel = storemodel.LiveOrderModel
type tradeOperationModel = storemodel.TradeOperationModel

type (
	StrategyInstanceRecord  = database.StrategyInstanceRecord
	StrategyStatus          = database.StrategyStatus
	StrategyChangeLogRecord = database.StrategyChangeLogRecord
	LiveOrderRecord         = database.LiveOrderRecord
	LiveOrderStatus         = database.LiveOrderStatus
	TradeOperationRecord    = database.TradeOperationRecord
	OperationType           = database.OperationType
	EventRecord             = database.EventRecord
	LivePositionStore       = database.LivePositionStore
)

// GormStore implements strategy and position storage using Gorm + SQLite.
type GormStore struct {
	db *gorm.DB
}

// GormStrategyStore is an alias for backward compatibility.
type GormStrategyStore = GormStore

// NewGormStore initializes a new GormStore instance.
func NewGormStore(path string) (*GormStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("gorm store: 决策日志路径不能为空")
	}
	if err := ensureDir(path); err != nil {
		return nil, err
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&cache=shared", path)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger:                                   logger.Default.LogMode(logger.Silent),
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		return nil, err
	}
	models := []interface{}{
		&strategyInstanceModel{},
		&strategyChangeLogModel{},
		&liveOrderModel{},
		&liveOrderModel{},
		&tradeOperationModel{},
		&eventLogModel{},
	}
	if err := db.AutoMigrate(models...); err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	// SQLite + WAL: allow a small amount of parallelism for concurrent HTTP reads
	// while keeping lock contention low.
	sqlDB.SetMaxOpenConns(2)
	sqlDB.SetMaxIdleConns(2)
	return &GormStore{db: db}, nil
}

// NewGormStrategyStore is equivalent to NewGormStore.
func NewGormStrategyStore(path string) (*GormStore, error) {
	return NewGormStore(path)
}

// Close closes the underlying database connection.
func (s *GormStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// SQLDB exposes the underlying *sql.DB for shared connections.
func (s *GormStore) SQLDB() (*sql.DB, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("gorm store 未初始化")
	}
	return s.db.DB()
}

// GormDB exposes the underlying *gorm.DB (read-only reference).
func (s *GormStore) GormDB() *gorm.DB {
	if s == nil {
		return nil
	}
	return s.db
}

var (
	_ LivePositionStore = (*GormStore)(nil)
	_ LivePositionStore = (*GormStrategyStore)(nil)
)

// --------------------- StrategyInstance Implementation -------------------------

func (s *GormStore) InsertStrategyInstances(ctx context.Context, recs []StrategyInstanceRecord) error {
	if s == nil || s.db == nil || len(recs) == 0 {
		return nil
	}
	now := time.Now()
	models := make([]strategyInstanceModel, 0, len(recs))
	for _, rec := range recs {
		normalizeStrategyRecord(&rec, now)
		models = append(models, newStrategyInstanceModel(rec))
	}
	updates := clause.Assignments(map[string]interface{}{
		"params_json":       gorm.Expr("excluded.params_json"),
		"plan_version":      gorm.Expr("excluded.plan_version"),
		"state_json":        gorm.Expr("COALESCE(excluded.state_json, strategy_instances.state_json)"),
		"status":            gorm.Expr("excluded.status"),
		"decision_trace_id": gorm.Expr("COALESCE(excluded.decision_trace_id, strategy_instances.decision_trace_id)"),
		"last_eval_at":      gorm.Expr("COALESCE(excluded.last_eval_at, strategy_instances.last_eval_at)"),
		"next_eval_after":   gorm.Expr("COALESCE(excluded.next_eval_after, strategy_instances.next_eval_after)"),
		"updated_at":        gorm.Expr("excluded.updated_at"),
	})
	return s.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "trade_id"}, {Name: "plan_id"}, {Name: "plan_component"}},
			DoUpdates: updates,
		}).
		Create(&models).Error
}

func (s *GormStore) ListStrategyInstances(ctx context.Context, tradeID int) ([]StrategyInstanceRecord, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("gorm store 未初始化")
	}
	if tradeID <= 0 {
		return nil, fmt.Errorf("trade_id 必填")
	}
	var models []strategyInstanceModel
	if err := s.db.WithContext(ctx).Where("trade_id = ?", tradeID).Find(&models).Error; err != nil {
		return nil, err
	}
	records := make([]StrategyInstanceRecord, 0, len(models))
	for _, m := range models {
		records = append(records, strategyInstanceModelToRecord(m))
	}
	return records, nil
}

// ListStrategyInstancesByTradeIDs batches strategy_instances lookup for multiple trades.
// This avoids N+1 queries for list endpoints (e.g. desk positions snapshot).
func (s *GormStore) ListStrategyInstancesByTradeIDs(ctx context.Context, tradeIDs []int) (map[int][]StrategyInstanceRecord, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("gorm store 未初始化")
	}
	ids := make([]int, 0, len(tradeIDs))
	seen := make(map[int]struct{}, len(tradeIDs))
	for _, id := range tradeIDs {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	out := make(map[int][]StrategyInstanceRecord, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var models []strategyInstanceModel
	if err := s.db.WithContext(ctx).Where("trade_id IN ?", ids).Find(&models).Error; err != nil {
		return nil, err
	}
	for _, m := range models {
		rec := strategyInstanceModelToRecord(m)
		out[rec.TradeID] = append(out[rec.TradeID], rec)
	}
	return out, nil
}

func (s *GormStore) UpdateStrategyInstanceState(ctx context.Context, tradeID int, planID, component, stateJSON string, status StrategyStatus) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("gorm store 未初始化")
	}
	planID = strings.TrimSpace(planID)
	component = strings.TrimSpace(component)
	if tradeID <= 0 || planID == "" {
		return fmt.Errorf("plan 更新参数缺失 trade_id=%d plan_id=%s", tradeID, planID)
	}
	if strings.TrimSpace(stateJSON) == "" {
		stateJSON = "{}"
	}
	payload := map[string]interface{}{
		"state_json": datatypes.JSON(mustJSONBytes(stateJSON)),
		"status":     status,
		"updated_at": time.Now().Unix(),
	}
	res := s.db.WithContext(ctx).Model(&strategyInstanceModel{}).
		Where("trade_id = ? AND plan_id = ? AND plan_component = ?", tradeID, planID, component).
		Updates(payload)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (s *GormStore) ActiveTradeIDs(ctx context.Context) ([]int, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("gorm store 未初始化")
	}
	var ids []int
	// JOIN with live_orders to ensure we only pick up trades that are NOT closed.
	// This adds a safety layer against zombie strategies if FinalizeStrategies wasn't called.
	err := s.db.WithContext(ctx).
		Table("strategy_instances").
		Joins("JOIN live_orders ON live_orders.freqtrade_id = strategy_instances.trade_id").
		Where("strategy_instances.status != ?", database.StrategyStatusDone).
		Where("live_orders.status NOT IN (?, ?)", database.LiveOrderStatusClosed, database.LiveOrderStatusClosingFull).
		Distinct("strategy_instances.trade_id").
		Pluck("strategy_instances.trade_id", &ids).Error

	if err != nil {
		return nil, err
	}
	return ids, nil
}

// FinalizeStrategies marks all active strategies for a trade as Done.
func (s *GormStore) FinalizeStrategies(ctx context.Context, tradeID int) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("gorm store 未初始化")
	}
	if tradeID <= 0 {
		return fmt.Errorf("invalid trade_id")
	}
	return s.db.WithContext(ctx).Model(&strategyInstanceModel{}).
		Where("trade_id = ? AND status != ?", tradeID, database.StrategyStatusDone).
		Updates(map[string]interface{}{
			"status":     database.StrategyStatusDone,
			"updated_at": time.Now().Unix(),
		}).Error
}

// FinalizePendingStrategies marks the oldest Pending tier component as Done.
// Called on exit_fill webhook to confirm a single tier close was successful.
func (s *GormStore) FinalizePendingStrategies(ctx context.Context, tradeID int) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("gorm store 未初始化")
	}
	if tradeID <= 0 {
		return fmt.Errorf("invalid trade_id")
	}
	var model strategyInstanceModel
	err := s.db.WithContext(ctx).
		Where("trade_id = ? AND status = ? AND plan_component != ''", tradeID, database.StrategyStatusPending).
		Order("updated_at ASC, id ASC").
		First(&model).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	return s.db.WithContext(ctx).Model(&strategyInstanceModel{}).
		Where("id = ?", model.ID).
		Updates(map[string]interface{}{
			"status":     database.StrategyStatusDone,
			"updated_at": time.Now().Unix(),
		}).Error
}

func (s *GormStore) InsertStrategyChangeLog(ctx context.Context, rec StrategyChangeLogRecord) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("gorm store 未初始化")
	}
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now()
	}
	model := strategyChangeLogModel{
		TradeID:         rec.TradeID,
		InstanceID:      rec.InstanceID,
		PlanID:          strings.TrimSpace(rec.PlanID),
		PlanComponent:   strings.TrimSpace(rec.PlanComponent),
		ChangedField:    strings.TrimSpace(rec.ChangedField),
		OldValue:        strings.TrimSpace(rec.OldValue),
		NewValue:        strings.TrimSpace(rec.NewValue),
		TriggerSource:   strings.TrimSpace(rec.TriggerSource),
		Reason:          strings.TrimSpace(rec.Reason),
		DecisionTraceID: strings.TrimSpace(rec.DecisionTraceID),
		CreatedAtUnix:   rec.CreatedAt.Unix(),
	}
	return s.db.WithContext(ctx).Create(&model).Error
}

// --------------------- LivePositionStore Implementation -----------------------

func (s *GormStore) UpsertLiveOrder(ctx context.Context, rec LiveOrderRecord) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("gorm store 未初始化")
	}
	if rec.FreqtradeID <= 0 {
		return fmt.Errorf("freqtrade_id 必填")
	}
	model := newLiveOrderModel(rec)
	cols := []string{
		"symbol", "side", "amount", "initial_amount", "stake_amount", "leverage", "position_value",
		"price", "closed_amount", "pnl_ratio", "pnl_usd", "current_price", "current_profit_ratio",
		"current_profit_abs", "unrealized_pnl_ratio", "unrealized_pnl_usd", "realized_pnl_ratio",
		"realized_pnl_usd", "is_simulated", "status", "start_timestamp", "end_timestamp",
		"last_status_sync", "raw_data", "updated_at",
	}
	return s.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "freqtrade_id"}},
			DoUpdates: clause.AssignmentColumns(cols),
		}).
		Create(&model).Error
}

func (s *GormStore) UpdateOrderStatus(ctx context.Context, tradeID int, status LiveOrderStatus) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("gorm store 未初始化")
	}
	if tradeID <= 0 {
		return fmt.Errorf("freqtrade_id 必填")
	}
	if status == 0 {
		status = database.LiveOrderStatusOpen
	}
	res := s.db.WithContext(ctx).Model(&liveOrderModel{}).
		Where("freqtrade_id = ?", tradeID).
		Updates(map[string]interface{}{
			"status":     status,
			"updated_at": time.Now().UnixMilli(),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	// 当订单状态变为 Closed 时，主动清理对应的策略实例
	if status == database.LiveOrderStatusClosed {
		_ = s.FinalizeStrategies(ctx, tradeID)
	}
	return nil
}

func (s *GormStore) SavePosition(ctx context.Context, order LiveOrderRecord) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("gorm store 未初始化")
	}
	if order.FreqtradeID <= 0 {
		return fmt.Errorf("freqtrade_id 必填")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "freqtrade_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"symbol", "side", "amount", "initial_amount", "stake_amount", "leverage", "position_value",
				"price", "closed_amount", "pnl_ratio", "pnl_usd", "current_price", "current_profit_ratio",
				"current_profit_abs", "unrealized_pnl_ratio", "unrealized_pnl_usd", "realized_pnl_ratio",
				"realized_pnl_usd", "is_simulated", "status", "start_timestamp", "end_timestamp",
				"last_status_sync", "raw_data", "updated_at",
			}),
		}).Create(ptrToLiveOrderModel(newLiveOrderModel(order))).Error
	})
}

func ptrToLiveOrderModel(m liveOrderModel) *liveOrderModel {
	return &m
}

func (s *GormStore) AppendTradeOperation(ctx context.Context, op TradeOperationRecord) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("gorm store 未初始化")
	}
	detailBytes, _ := json.Marshal(op.Details)
	model := tradeOperationModel{
		FreqtradeID: op.FreqtradeID,
		Symbol:      strings.ToUpper(strings.TrimSpace(op.Symbol)),
		Operation:   int(op.Operation),
		Details:     datatypes.JSON(detailBytes),
		Timestamp:   op.Timestamp.UnixMilli(),
	}
	return s.db.WithContext(ctx).Create(&model).Error
}

func (s *GormStore) ListTradeOperations(ctx context.Context, tradeID int, limit int) ([]TradeOperationRecord, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("gorm store 未初始化")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var models []tradeOperationModel
	if err := s.db.WithContext(ctx).
		Where("freqtrade_id = ?", tradeID).
		Order("timestamp DESC, id DESC").
		Limit(limit).
		Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]TradeOperationRecord, 0, len(models))
	for _, m := range models {
		out = append(out, tradeOperationModelToRecord(m))
	}
	return out, nil
}

func (s *GormStore) GetLivePosition(ctx context.Context, tradeID int) (LiveOrderRecord, bool, error) {
	if s == nil || s.db == nil {
		return LiveOrderRecord{}, false, fmt.Errorf("gorm store 未初始化")
	}
	var order liveOrderModel
	if err := s.db.WithContext(ctx).Where("freqtrade_id = ?", tradeID).First(&order).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return LiveOrderRecord{}, false, nil
		}
		return LiveOrderRecord{}, false, err
	}
	return liveOrderModelToRecord(order), true, nil
}

func (s *GormStore) ListActivePositions(ctx context.Context, limit int) ([]LiveOrderRecord, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("gorm store 未初始化")
	}
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	var orders []liveOrderModel
	statuses := []int{
		int(database.LiveOrderStatusOpen),
		int(database.LiveOrderStatusPartial),
		int(database.LiveOrderStatusRetrying),
		int(database.LiveOrderStatusOpening),
		int(database.LiveOrderStatusClosingPartial),
		int(database.LiveOrderStatusClosingFull),
	}
	if err := s.db.WithContext(ctx).
		Where("status IN ?", statuses).
		Order("start_timestamp DESC, id DESC").
		Limit(limit).
		Find(&orders).Error; err != nil {
		return nil, err
	}
	out := make([]LiveOrderRecord, 0, len(orders))
	for _, o := range orders {
		out = append(out, liveOrderModelToRecord(o))
	}
	return out, nil
}

func (s *GormStore) ListRecentPositions(ctx context.Context, limit int) ([]LiveOrderRecord, error) {
	return s.ListRecentPositionsPaged(ctx, "", limit, 0)
}

func (s *GormStore) ListRecentPositionsPaged(ctx context.Context, symbol string, limit int, offset int) ([]LiveOrderRecord, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("gorm store 未初始化")
	}
	if limit <= 0 || limit > 500 {
		limit = 10
	}
	if offset < 0 {
		offset = 0
	}
	query := s.db.WithContext(ctx).Model(&liveOrderModel{})
	if sym := strings.ToUpper(strings.TrimSpace(symbol)); sym != "" {
		query = query.Where("UPPER(symbol) = ?", sym)
	}
	var orders []liveOrderModel
	if err := query.
		Order("COALESCE(end_timestamp, start_timestamp, created_at) DESC, id DESC").
		Limit(limit).
		Offset(offset).
		Find(&orders).Error; err != nil {
		return nil, err
	}
	out := make([]LiveOrderRecord, 0, len(orders))
	for _, o := range orders {
		out = append(out, liveOrderModelToRecord(o))
	}
	return out, nil
}

func (s *GormStore) CountRecentPositions(ctx context.Context, symbol string) (int, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("gorm store 未初始化")
	}
	query := s.db.WithContext(ctx).Model(&liveOrderModel{})
	if sym := strings.ToUpper(strings.TrimSpace(symbol)); sym != "" {
		query = query.Where("UPPER(symbol) = ?", sym)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return 0, err
	}
	return int(total), nil
}

func (s *GormStore) AddOrderPnLColumns() error {
	// gorm.AutoMigrate already handles fields.
	return nil
}

// --------------------- Event Sourcing Implementation ----------------------

func (s *GormStore) AppendEvent(ctx context.Context, evt EventRecord) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("gorm store 未初始化")
	}
	model := eventLogModel{
		EventID:       evt.ID,
		Type:          evt.Type,
		TradeID:       evt.TradeID,
		Symbol:        strings.ToUpper(strings.TrimSpace(evt.Symbol)),
		Payload:       datatypes.JSON(evt.Payload),
		CreatedAtUnix: evt.CreatedAt.UnixMilli(),
	}
	return s.db.WithContext(ctx).Create(&model).Error
}

func (s *GormStore) LoadEvents(ctx context.Context, since time.Time, limit int) ([]EventRecord, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("gorm store 未初始化")
	}
	if limit <= 0 {
		limit = 1000
	}
	var models []eventLogModel
	query := s.db.WithContext(ctx).Order("created_at ASC").Limit(limit)

	if !since.IsZero() {
		query = query.Where("created_at > ?", since.UnixMilli())
	}

	if err := query.Find(&models).Error; err != nil {
		return nil, err
	}

	out := make([]EventRecord, 0, len(models))
	for _, m := range models {
		out = append(out, EventRecord{
			ID:        m.EventID,
			Type:      m.Type,
			Payload:   []byte(m.Payload),
			CreatedAt: time.UnixMilli(m.CreatedAtUnix),
			TradeID:   m.TradeID,
			Symbol:    m.Symbol,
		})
	}
	return out, nil
}

// --------------------------- Model Helpers ------------------------------

func ensureDir(path string) error {
	dir := filepathDir(path)
	if dir == "" {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}

type strategyChangeLogModel struct {
	ID              int64  `gorm:"column:id;primaryKey"`
	TradeID         int    `gorm:"column:trade_id;index"`
	InstanceID      int64  `gorm:"column:instance_id"`
	PlanID          string `gorm:"column:plan_id"`
	PlanComponent   string `gorm:"column:plan_component"`
	ChangedField    string `gorm:"column:changed_field"`
	OldValue        string `gorm:"column:old_value"`
	NewValue        string `gorm:"column:new_value"`
	TriggerSource   string `gorm:"column:trigger_source"`
	Reason          string `gorm:"column:reason"`
	DecisionTraceID string `gorm:"column:decision_trace_id"`
	CreatedAtUnix   int64  `gorm:"column:created_at"`
}

func (strategyChangeLogModel) TableName() string { return "strategy_change_log" }

func newStrategyInstanceModel(rec StrategyInstanceRecord) strategyInstanceModel {
	now := time.Now()
	normalizeStrategyRecord(&rec, now)
	model := strategyInstanceModel{
		TradeID:         rec.TradeID,
		PlanID:          strings.TrimSpace(rec.PlanID),
		PlanComponent:   strings.TrimSpace(rec.PlanComponent),
		PlanVersion:     rec.PlanVersion,
		ParamsJSON:      datatypes.JSON(mustJSONBytes(rec.ParamsJSON)),
		StateJSON:       datatypes.JSON(mustJSONBytes(rec.StateJSON)),
		Status:          rec.Status,
		DecisionTraceID: strings.TrimSpace(rec.DecisionTraceID),
		CreatedAtUnix:   rec.CreatedAt.Unix(),
		UpdatedAtUnix:   rec.UpdatedAt.Unix(),
	}
	if rec.LastEvalAt != nil && !rec.LastEvalAt.IsZero() {
		val := rec.LastEvalAt.Unix()
		model.LastEvalUnix = &val
	}
	if rec.NextEvalAfter != nil && !rec.NextEvalAfter.IsZero() {
		val := rec.NextEvalAfter.Unix()
		model.NextEvalUnix = &val
	}
	return model
}

func strategyInstanceModelToRecord(m strategyInstanceModel) StrategyInstanceRecord {
	rec := StrategyInstanceRecord{
		ID:              m.ID,
		TradeID:         m.TradeID,
		PlanID:          strings.TrimSpace(m.PlanID),
		PlanComponent:   strings.TrimSpace(m.PlanComponent),
		PlanVersion:     m.PlanVersion,
		ParamsJSON:      jsonBytesToString(m.ParamsJSON),
		StateJSON:       jsonBytesToString(m.StateJSON),
		Status:          m.Status,
		DecisionTraceID: strings.TrimSpace(m.DecisionTraceID),
		CreatedAt:       time.Unix(m.CreatedAtUnix, 0),
		UpdatedAt:       time.Unix(m.UpdatedAtUnix, 0),
	}
	if m.LastEvalUnix != nil && *m.LastEvalUnix > 0 {
		ts := time.Unix(*m.LastEvalUnix, 0)
		rec.LastEvalAt = &ts
	}
	if m.NextEvalUnix != nil && *m.NextEvalUnix > 0 {
		ts := time.Unix(*m.NextEvalUnix, 0)
		rec.NextEvalAfter = &ts
	}
	return rec
}

func normalizeStrategyRecord(rec *StrategyInstanceRecord, now time.Time) {
	if rec == nil {
		return
	}
	if now.IsZero() {
		now = time.Now()
	}
	if rec.TradeID <= 0 {
		panic("strategy instance requires trade_id")
	}
	rec.PlanID = strings.TrimSpace(rec.PlanID)
	if rec.PlanComponent == "" {
		rec.PlanComponent = ""
	}
	if rec.PlanVersion <= 0 {
		rec.PlanVersion = 1
	}
	if strings.TrimSpace(rec.ParamsJSON) == "" {
		rec.ParamsJSON = "{}"
	}
	if strings.TrimSpace(rec.StateJSON) == "" {
		rec.StateJSON = "{}"
	}
	if rec.Status == 0 && rec.Status != database.StrategyStatusWaiting {
		rec.Status = database.StrategyStatusWaiting
	}
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = now
	}
	if rec.UpdatedAt.IsZero() {
		rec.UpdatedAt = now
	}
}

type eventLogModel struct {
	ID            int64          `gorm:"column:id;primaryKey"`
	EventID       string         `gorm:"column:event_uuid;index"`
	Type          string         `gorm:"column:type"`
	TradeID       int            `gorm:"column:trade_id;index"`
	Symbol        string         `gorm:"column:symbol;index"`
	Payload       datatypes.JSON `gorm:"column:payload"`
	CreatedAtUnix int64          `gorm:"column:created_at;index"`
}

func (eventLogModel) TableName() string { return "event_log" }

// --- Model Conversion Helpers ---

func newLiveOrderModel(rec LiveOrderRecord) liveOrderModel {
	now := time.Now()
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = now
	}
	if rec.UpdatedAt.IsZero() {
		rec.UpdatedAt = now
	}
	if rec.StartTime == nil || rec.StartTime.IsZero() {
		tmp := rec.CreatedAt
		rec.StartTime = &tmp
	}
	return liveOrderModel{
		ID:                int64(rec.FreqtradeID),
		FreqtradeID:       rec.FreqtradeID,
		Symbol:            strings.ToUpper(strings.TrimSpace(rec.Symbol)),
		Side:              strings.ToLower(strings.TrimSpace(rec.Side)),
		Amount:            valOrZero(rec.Amount),
		InitialAmount:     valOrZero(rec.InitialAmount),
		StakeAmount:       valOrZero(rec.StakeAmount),
		Leverage:          valOrZero(rec.Leverage),
		PositionValue:     valOrZero(rec.PositionValue),
		Price:             valOrZero(rec.Price),
		ClosedAmount:      valOrZero(rec.ClosedAmount),
		PnLRatio:          valOrZero(rec.PnLRatio),
		PnLUSD:            valOrZero(rec.PnLUSD),
		CurrentPrice:      valOrZero(rec.CurrentPrice),
		CurrentProfitRate: valOrZero(rec.CurrentProfitRatio),
		CurrentProfitAbs:  valOrZero(rec.CurrentProfitAbs),
		UnrealizedRatio:   valOrZero(rec.UnrealizedPnLRatio),
		UnrealizedUSD:     valOrZero(rec.UnrealizedPnLUSD),
		RealizedRatio:     valOrZero(rec.RealizedPnLRatio),
		RealizedUSD:       valOrZero(rec.RealizedPnLUSD),
		IsSimulated:       boolPtrToInt(rec.IsSimulated),
		Status:            rec.Status,
		StartTimestamp:    timeToMillis(rec.StartTime),
		EndTimestamp:      timeToMillis(rec.EndTime),
		LastStatusSync:    timeToMillis(rec.LastStatusSync),
		RawData:           strings.TrimSpace(rec.RawData),
		CreatedAtUnix:     rec.CreatedAt.UnixMilli(),
		UpdatedAtUnix:     rec.UpdatedAt.UnixMilli(),
	}
}

func liveOrderModelToRecord(m liveOrderModel) LiveOrderRecord {
	rec := LiveOrderRecord{
		FreqtradeID: m.FreqtradeID,
		Symbol:      strings.ToUpper(strings.TrimSpace(m.Symbol)),
		Side:        strings.ToLower(strings.TrimSpace(m.Side)),
		Status:      LiveOrderStatus(m.Status),
		RawData:     m.RawData,
		CreatedAt:   millisToTime(m.CreatedAtUnix),
		UpdatedAt:   millisToTime(m.UpdatedAtUnix),
	}
	if m.StartTimestamp > 0 {
		ts := millisToTime(m.StartTimestamp)
		rec.StartTime = &ts
	}
	if m.EndTimestamp > 0 {
		ts := millisToTime(m.EndTimestamp)
		rec.EndTime = &ts
	}
	if m.LastStatusSync > 0 {
		ts := millisToTime(m.LastStatusSync)
		rec.LastStatusSync = &ts
	}
	rec.Amount = ptrFloat(m.Amount)
	rec.InitialAmount = ptrFloat(m.InitialAmount)
	rec.StakeAmount = ptrFloat(m.StakeAmount)
	rec.Leverage = ptrFloat(m.Leverage)
	rec.PositionValue = ptrFloat(m.PositionValue)
	rec.Price = ptrFloat(m.Price)
	rec.ClosedAmount = ptrFloat(m.ClosedAmount)
	rec.PnLRatio = ptrFloat(m.PnLRatio)
	rec.PnLUSD = ptrFloat(m.PnLUSD)
	rec.CurrentPrice = ptrFloat(m.CurrentPrice)
	rec.CurrentProfitRatio = ptrFloat(m.CurrentProfitRate)
	rec.CurrentProfitAbs = ptrFloat(m.CurrentProfitAbs)
	rec.UnrealizedPnLRatio = ptrFloat(m.UnrealizedRatio)
	rec.UnrealizedPnLUSD = ptrFloat(m.UnrealizedUSD)
	rec.RealizedPnLRatio = ptrFloat(m.RealizedRatio)
	rec.RealizedPnLUSD = ptrFloat(m.RealizedUSD)
	rec.IsSimulated = ptrBool(m.IsSimulated != 0)
	return rec
}

func tradeOperationModelToRecord(m tradeOperationModel) TradeOperationRecord {
	var details map[string]any
	if len(m.Details) > 0 {
		_ = json.Unmarshal(m.Details, &details)
	}
	return TradeOperationRecord{
		FreqtradeID: m.FreqtradeID,
		Symbol:      m.Symbol,
		Operation:   OperationType(m.Operation),
		Details:     details,
		Timestamp:   millisToTime(m.Timestamp),
	}
}

// --------------------------- Helper Functions ------------------------------------

func mustJSONBytes(raw string) []byte {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []byte("{}")
	}
	return []byte(raw)
}

func jsonBytesToString(data datatypes.JSON) string {
	if len(data) == 0 {
		return "{}"
	}
	return string(data)
}

func valOrZero(ptr *float64) float64 {
	if ptr != nil {
		return *ptr
	}
	return 0
}

func ptrFloat(v float64) *float64 {
	return &v
}

func ptrBool(v bool) *bool {
	return &v
}

func boolPtrToInt(ptr *bool) int {
	if ptr != nil && *ptr {
		return 1
	}
	return 0
}

func timeToMillis(t *time.Time) int64 {
	if t == nil || t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

func millisToTime(v int64) time.Time {
	if v <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(v)
}

func filepathDir(path string) string {
	last := strings.LastIndex(path, "/")
	if last == -1 {
		last = strings.LastIndex(path, "\\")
	}
	if last == -1 {
		return ""
	}
	return path[:last]
}
