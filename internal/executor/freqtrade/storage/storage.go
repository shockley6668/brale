package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// RiskRecord 保存 Brale→Freqtrade 共享的止盈止损信息。
type RiskRecord struct {
	TradeID    int
	Symbol     string
	Pair       string
	Side       string
	EntryPrice float64
	Stake      float64
	Amount     float64
	Leverage   float64
	StopLoss   float64
	TakeProfit float64
	Reason     string
	Source     string
	Status     string
	UpdatedAt  time.Time
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
			stop_loss, take_profit, reason, source, status, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
			updated_at=excluded.updated_at;
	`, rec.TradeID, rec.Symbol, rec.Pair, rec.Side, nullIfZero(rec.EntryPrice), nullIfZero(rec.Stake), nullIfZero(rec.Amount),
		nullIfZero(rec.Leverage), nullIfZero(rec.StopLoss), nullIfZero(rec.TakeProfit),
		nullIfEmpty(rec.Reason), nullIfEmpty(rec.Source), nullIfEmpty(rec.Status), now.UnixMilli())
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
		updated_at INTEGER NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_trade_risk_symbol ON trade_risk(symbol);
	`
	_, err := db.Exec(stmt)
	return err
}

func nullIfZero(val float64) interface{} {
	if val == 0 {
		return nil
	}
	return val
}

func nullIfEmpty(s string) interface{} {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return s
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
		       stop_loss, take_profit, reason, source, status, updated_at
		FROM trade_risk WHERE trade_id = ?`, tradeID)
	var rec RiskRecord
	var updated sql.NullInt64
	var entry sql.NullFloat64
	var stake sql.NullFloat64
	var amount sql.NullFloat64
	var lev sql.NullFloat64
	var stop sql.NullFloat64
	var take sql.NullFloat64
	err := row.Scan(&rec.TradeID, &rec.Symbol, &rec.Pair, &rec.Side, &entry,
		&stake, &amount, &lev, &stop, &take,
		&rec.Reason, &rec.Source, &rec.Status, &updated)
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
	return rec, true, nil
}
