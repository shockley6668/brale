package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// RiskRecord 保存 Brale→Freqtrade 共享的止盈止损信息。
type RiskRecord struct {
	TradeID        int
	Symbol         string
	Pair           string
	Side           string
	EntryPrice     float64
	Stake          float64
	Amount         float64
	Leverage       float64
	StopLoss       float64
	TakeProfit     float64
	Reason         string
	Source         string
	Status         string
	UpdatedAt      time.Time
	Tier1Target    float64
	Tier1Ratio     float64
	Tier1Done      bool
	Tier2Target    float64
	Tier2Ratio     float64
	Tier2Done      bool
	Tier3Target    float64
	Tier3Ratio     float64
	Tier3Done      bool
	RemainingRatio float64
	TierNotes      string
}

// TierLog 记录三段式配置或触发的历史。
type TierLog struct {
	TradeID   int       `json:"trade_id"`
	TierName  string    `json:"tier_name"`
	OldTarget float64   `json:"old_target"`
	NewTarget float64   `json:"new_target"`
	OldRatio  float64   `json:"old_ratio"`
	NewRatio  float64   `json:"new_ratio"`
	Reason    string    `json:"reason"`
	Source    string    `json:"source"`
	CreatedAt time.Time `json:"created_at"`
}

// TradeEvent 记录仓位关键操作的时间线。
type TradeEvent struct {
	ID             int64     `json:"id"`
	TradeID        int       `json:"trade_id"`
	Event          string    `json:"event"`
	Detail         string    `json:"detail,omitempty"`
	StopLoss       float64   `json:"stop_loss,omitempty"`
	TakeProfit     float64   `json:"take_profit,omitempty"`
	Tier1Target    float64   `json:"tier1_target,omitempty"`
	Tier2Target    float64   `json:"tier2_target,omitempty"`
	Tier3Target    float64   `json:"tier3_target,omitempty"`
	RemainingRatio float64   `json:"remaining_ratio,omitempty"`
	Source         string    `json:"source,omitempty"`
	Extra          string    `json:"extra,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// Store wraps a sqlite database for risk records.
type Store struct {
	mu   sync.Mutex
	db   *sql.DB
	path string
}

// Open opens or creates the sqlite database.
func Open(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("storage path 不能为空")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建存储目录失败: %w", err)
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&cache=shared", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := ensureSchema(db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db, path: path}, nil
}

// Close closes the underlying db.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	return err
}

// Upsert records a new stop/take info entry.
func (s *Store) Upsert(ctx context.Context, rec RiskRecord) error {
	s.mu.Lock()
	db := s.db
	s.mu.Unlock()
	if db == nil {
		return fmt.Errorf("storage 未初始化")
	}
	if rec.TradeID <= 0 {
		return fmt.Errorf("trade_id 需 > 0")
	}
	now := rec.UpdatedAt
	if now.IsZero() {
		now = time.Now()
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO trade_risk(trade_id, symbol, pair, side, entry_price, stake_amount, amount, leverage,
			stop_loss, take_profit, reason, source, status, updated_at,
			tier1_target, tier1_ratio, tier1_done,
			tier2_target, tier2_ratio, tier2_done,
			tier3_target, tier3_ratio, tier3_done,
			remaining_ratio, tier_notes)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(trade_id) DO UPDATE SET
			symbol=excluded.symbol,
			pair=excluded.pair,
			side=excluded.side,
			entry_price=excluded.entry_price,
			stake_amount=excluded.stake_amount,
			amount=excluded.amount,
			leverage=excluded.leverage,
			stop_loss=excluded.stop_loss,
			take_profit=excluded.take_profit,
			reason=excluded.reason,
			source=excluded.source,
			status=excluded.status,
			updated_at=excluded.updated_at,
			tier1_target=excluded.tier1_target,
			tier1_ratio=excluded.tier1_ratio,
			tier1_done=excluded.tier1_done,
			tier2_target=excluded.tier2_target,
			tier2_ratio=excluded.tier2_ratio,
			tier2_done=excluded.tier2_done,
			tier3_target=excluded.tier3_target,
			tier3_ratio=excluded.tier3_ratio,
			tier3_done=excluded.tier3_done,
			remaining_ratio=excluded.remaining_ratio,
			tier_notes=excluded.tier_notes;
	`, rec.TradeID, rec.Symbol, rec.Pair, rec.Side, nullIfZero(rec.EntryPrice), nullIfZero(rec.Stake), nullIfZero(rec.Amount),
		nullIfZero(rec.Leverage), nullIfZero(rec.StopLoss), nullIfZero(rec.TakeProfit),
		nullIfEmpty(rec.Reason), nullIfEmpty(rec.Source), nullIfEmpty(rec.Status), now.UnixMilli(),
		nullIfZero(rec.Tier1Target), nullIfZero(rec.Tier1Ratio), boolToInt(rec.Tier1Done),
		nullIfZero(rec.Tier2Target), nullIfZero(rec.Tier2Ratio), boolToInt(rec.Tier2Done),
		nullIfZero(rec.Tier3Target), nullIfZero(rec.Tier3Ratio), boolToInt(rec.Tier3Done),
		nullIfZero(rec.RemainingRatio), nullIfEmpty(rec.TierNotes))
	return err
}

func ensureSchema(db *sql.DB) error {
	stmt := `
	CREATE TABLE IF NOT EXISTS trade_risk (
		trade_id INTEGER PRIMARY KEY,
		symbol TEXT,
		pair TEXT,
		side TEXT,
		entry_price REAL,
		stake_amount REAL,
		amount REAL,
		leverage REAL,
		stop_loss REAL,
		take_profit REAL,
		reason TEXT,
		source TEXT,
		status TEXT,
		updated_at INTEGER NOT NULL,
		tier1_target REAL,
		tier1_ratio REAL,
		tier1_done INTEGER,
		tier2_target REAL,
		tier2_ratio REAL,
		tier2_done INTEGER,
		tier3_target REAL,
		tier3_ratio REAL,
		tier3_done INTEGER,
		remaining_ratio REAL,
		tier_notes TEXT
	);
	CREATE INDEX IF NOT EXISTS idx_trade_risk_symbol ON trade_risk(symbol);
	CREATE TABLE IF NOT EXISTS trade_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		trade_id INTEGER NOT NULL,
		event TEXT,
		detail TEXT,
		stop_loss REAL,
		take_profit REAL,
		tier1_target REAL,
		tier2_target REAL,
		tier3_target REAL,
		remaining_ratio REAL,
		source TEXT,
		extra TEXT,
		created_at INTEGER NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_trade_events_trade ON trade_events(trade_id);
	CREATE INDEX IF NOT EXISTS idx_trade_events_created ON trade_events(created_at);
	`
	if _, err := db.Exec(stmt); err != nil {
		return err
	}
	// 额外保障：对已有库补充缺失列
	if err := ensureRiskColumn(db, "tier1_target", "ALTER TABLE trade_risk ADD COLUMN tier1_target REAL"); err != nil {
		return err
	}
	if err := ensureRiskColumn(db, "tier1_ratio", "ALTER TABLE trade_risk ADD COLUMN tier1_ratio REAL"); err != nil {
		return err
	}
	if err := ensureRiskColumn(db, "tier1_done", "ALTER TABLE trade_risk ADD COLUMN tier1_done INTEGER"); err != nil {
		return err
	}
	if err := ensureRiskColumn(db, "tier2_target", "ALTER TABLE trade_risk ADD COLUMN tier2_target REAL"); err != nil {
		return err
	}
	if err := ensureRiskColumn(db, "tier2_ratio", "ALTER TABLE trade_risk ADD COLUMN tier2_ratio REAL"); err != nil {
		return err
	}
	if err := ensureRiskColumn(db, "tier2_done", "ALTER TABLE trade_risk ADD COLUMN tier2_done INTEGER"); err != nil {
		return err
	}
	if err := ensureRiskColumn(db, "tier3_target", "ALTER TABLE trade_risk ADD COLUMN tier3_target REAL"); err != nil {
		return err
	}
	if err := ensureRiskColumn(db, "tier3_ratio", "ALTER TABLE trade_risk ADD COLUMN tier3_ratio REAL"); err != nil {
		return err
	}
	if err := ensureRiskColumn(db, "tier3_done", "ALTER TABLE trade_risk ADD COLUMN tier3_done INTEGER"); err != nil {
		return err
	}
	if err := ensureRiskColumn(db, "remaining_ratio", "ALTER TABLE trade_risk ADD COLUMN remaining_ratio REAL"); err != nil {
		return err
	}
	if err := ensureRiskColumn(db, "tier_notes", "ALTER TABLE trade_risk ADD COLUMN tier_notes TEXT"); err != nil {
		return err
	}
	// tier log table
	logStmt := `
	CREATE TABLE IF NOT EXISTS trade_tier_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		trade_id INTEGER NOT NULL,
		tier_name TEXT,
		old_target REAL,
		new_target REAL,
		old_ratio REAL,
		new_ratio REAL,
		reason TEXT,
		source TEXT,
		created_at INTEGER NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_trade_tier_logs_trade ON trade_tier_logs(trade_id);
	`
	_, err := db.Exec(logStmt)
	return err
}

func ensureRiskColumn(db *sql.DB, name, alter string) error {
	var exists int
	row := db.QueryRow(`SELECT 1 FROM pragma_table_info('trade_risk') WHERE name = ?`, name)
	err := row.Scan(&exists)
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		_, execErr := db.Exec(alter)
		return execErr
	}
	return err
}

func nullIfZero(val float64) interface{} {
	if val == 0 {
		return nil
	}
	return val
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// InsertTierLog 记录 tier 的变更。
func (s *Store) InsertTierLog(ctx context.Context, log TierLog) error {
	s.mu.Lock()
	db := s.db
	s.mu.Unlock()
	if db == nil {
		return fmt.Errorf("storage 未初始化")
	}
	if log.TradeID <= 0 {
		return fmt.Errorf("trade_id 需>0")
	}
	created := log.CreatedAt
	if created.IsZero() {
		created = time.Now()
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO trade_tier_logs(trade_id, tier_name, old_target, new_target, old_ratio, new_ratio, reason, source, created_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, log.TradeID, strings.TrimSpace(log.TierName), nullIfZero(log.OldTarget), nullIfZero(log.NewTarget),
		nullIfZero(log.OldRatio), nullIfZero(log.NewRatio), nullIfEmpty(log.Reason), nullIfEmpty(log.Source), created.UnixMilli())
	return err
}

// ListTierLogs 返回指定 trade 的 tier 调整记录。
func (s *Store) ListTierLogs(ctx context.Context, tradeID int, limit int) ([]TierLog, error) {
	s.mu.Lock()
	db := s.db
	s.mu.Unlock()
	if db == nil {
		return nil, fmt.Errorf("storage 未初始化")
	}
	if tradeID <= 0 {
		return nil, fmt.Errorf("trade_id 需>0")
	}
	if limit <= 0 {
		limit = 50
	}
	rows, err := db.QueryContext(ctx, `
		SELECT trade_id, tier_name, old_target, new_target, old_ratio, new_ratio, reason, source, created_at
		FROM trade_tier_logs
		WHERE trade_id = ?
		ORDER BY created_at DESC
		LIMIT ?`, tradeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	logs := make([]TierLog, 0, limit)
	for rows.Next() {
		var log TierLog
		var created sql.NullInt64
		var (
			oldTarget sql.NullFloat64
			newTarget sql.NullFloat64
			oldRatio  sql.NullFloat64
			newRatio  sql.NullFloat64
		)
		if err := rows.Scan(&log.TradeID, &log.TierName, &oldTarget, &newTarget, &oldRatio, &newRatio, &log.Reason, &log.Source, &created); err != nil {
			return nil, err
		}
		if oldTarget.Valid {
			log.OldTarget = oldTarget.Float64
		}
		if newTarget.Valid {
			log.NewTarget = newTarget.Float64
		}
		if oldRatio.Valid {
			log.OldRatio = oldRatio.Float64
		}
		if newRatio.Valid {
			log.NewRatio = newRatio.Float64
		}
		if created.Valid {
			log.CreatedAt = time.UnixMilli(created.Int64)
		}
		logs = append(logs, log)
	}
	return logs, rows.Err()
}

func nullIfEmpty(s string) interface{} {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return s
}

// InsertEvent 追加一条仓位事件。
func (s *Store) InsertEvent(ctx context.Context, ev TradeEvent) error {
	s.mu.Lock()
	db := s.db
	s.mu.Unlock()
	if db == nil {
		return fmt.Errorf("storage 未初始化")
	}
	if ev.TradeID <= 0 {
		return fmt.Errorf("trade_id 需>0")
	}
	now := ev.CreatedAt
	if now.IsZero() {
		now = time.Now()
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO trade_events(trade_id, event, detail, stop_loss, take_profit,
			tier1_target, tier2_target, tier3_target, remaining_ratio, source, extra, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ev.TradeID, nullIfEmpty(ev.Event), nullIfEmpty(ev.Detail),
		nullIfZero(ev.StopLoss), nullIfZero(ev.TakeProfit),
		nullIfZero(ev.Tier1Target), nullIfZero(ev.Tier2Target), nullIfZero(ev.Tier3Target),
		nullIfZero(ev.RemainingRatio), nullIfEmpty(ev.Source), nullIfEmpty(ev.Extra),
		now.UnixMilli(),
	)
	return err
}

// ListEvents 返回指定 trade 的事件时间线（倒序）。
func (s *Store) ListEvents(ctx context.Context, tradeID int, limit int) ([]TradeEvent, error) {
	s.mu.Lock()
	db := s.db
	s.mu.Unlock()
	if db == nil {
		return nil, fmt.Errorf("storage 未初始化")
	}
	if tradeID <= 0 {
		return nil, fmt.Errorf("trade_id 需>0")
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := db.QueryContext(ctx, `
		SELECT id, event, detail, stop_loss, take_profit, tier1_target, tier2_target, tier3_target,
		       remaining_ratio, source, extra, created_at
		FROM trade_events
		WHERE trade_id = ?
		ORDER BY created_at DESC
		LIMIT ?`, tradeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := make([]TradeEvent, 0, limit)
	for rows.Next() {
		var ev TradeEvent
		var created sql.NullInt64
		if err := rows.Scan(&ev.ID, &ev.Event, &ev.Detail, &ev.StopLoss, &ev.TakeProfit, &ev.Tier1Target, &ev.Tier2Target, &ev.Tier3Target,
			&ev.RemainingRatio, &ev.Source, &ev.Extra, &created); err != nil {
			return nil, err
		}
		ev.TradeID = tradeID
		if created.Valid {
			ev.CreatedAt = time.UnixMilli(created.Int64)
		}
		events = append(events, ev)
	}
	return events, rows.Err()
}

// Get returns the record for a trade if present.
func (s *Store) Get(ctx context.Context, tradeID int) (RiskRecord, bool, error) {
	s.mu.Lock()
	db := s.db
	s.mu.Unlock()
	if db == nil {
		return RiskRecord{}, false, fmt.Errorf("storage 未初始化")
	}
	if tradeID <= 0 {
		return RiskRecord{}, false, fmt.Errorf("trade_id 需 > 0")
	}
	row := db.QueryRowContext(ctx, `
		SELECT trade_id, symbol, pair, side, entry_price, stake_amount, amount, leverage,
		       stop_loss, take_profit, reason, source, status, updated_at,
		       tier1_target, tier1_ratio, tier1_done,
		       tier2_target, tier2_ratio, tier2_done,
		       tier3_target, tier3_ratio, tier3_done,
		       remaining_ratio, tier_notes
		FROM trade_risk WHERE trade_id = ?`, tradeID)
	var rec RiskRecord
	var updated sql.NullInt64
	var entry sql.NullFloat64
	var stake sql.NullFloat64
	var amount sql.NullFloat64
	var lev sql.NullFloat64
	var stop sql.NullFloat64
	var take sql.NullFloat64
	var tier1Target, tier1Ratio, tier2Target, tier2Ratio, tier3Target, tier3Ratio, remaining sql.NullFloat64
	var tier1Done, tier2Done, tier3Done sql.NullInt64
	var notes sql.NullString
	err := row.Scan(&rec.TradeID, &rec.Symbol, &rec.Pair, &rec.Side, &entry,
		&stake, &amount, &lev, &stop, &take,
		&rec.Reason, &rec.Source, &rec.Status, &updated,
		&tier1Target, &tier1Ratio, &tier1Done,
		&tier2Target, &tier2Ratio, &tier2Done,
		&tier3Target, &tier3Ratio, &tier3Done,
		&remaining, &notes)
	if err == sql.ErrNoRows {
		return RiskRecord{}, false, nil
	}
	if err != nil {
		return RiskRecord{}, false, err
	}
	if entry.Valid {
		rec.EntryPrice = entry.Float64
	}
	if stake.Valid {
		rec.Stake = stake.Float64
	}
	if amount.Valid {
		rec.Amount = amount.Float64
	}
	if lev.Valid {
		rec.Leverage = lev.Float64
	}
	if stop.Valid {
		rec.StopLoss = stop.Float64
	}
	if take.Valid {
		rec.TakeProfit = take.Float64
	}
	if updated.Valid {
		rec.UpdatedAt = time.UnixMilli(updated.Int64)
	}
	if tier1Target.Valid {
		rec.Tier1Target = tier1Target.Float64
	}
	if tier1Ratio.Valid {
		rec.Tier1Ratio = tier1Ratio.Float64
	}
	if tier2Target.Valid {
		rec.Tier2Target = tier2Target.Float64
	}
	if tier2Ratio.Valid {
		rec.Tier2Ratio = tier2Ratio.Float64
	}
	if tier3Target.Valid {
		rec.Tier3Target = tier3Target.Float64
	}
	if tier3Ratio.Valid {
		rec.Tier3Ratio = tier3Ratio.Float64
	}
	if remaining.Valid {
		rec.RemainingRatio = remaining.Float64
	}
	if tier1Done.Valid {
		rec.Tier1Done = tier1Done.Int64 == 1
	}
	if tier2Done.Valid {
		rec.Tier2Done = tier2Done.Int64 == 1
	}
	if tier3Done.Valid {
		rec.Tier3Done = tier3Done.Int64 == 1
	}
	if notes.Valid {
		rec.TierNotes = notes.String
	}
	return rec, true, nil
}
