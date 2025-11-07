package backtest

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Manifest 记录某个 symbol@timeframe 文件的统计信息。
type Manifest struct {
	Symbol     string `json:"symbol"`
	Timeframe  string `json:"timeframe"`
	MinTime    int64  `json:"min_time"`
	MaxTime    int64  `json:"max_time"`
	Rows       int64  `json:"rows"`
	LastSyncAt int64  `json:"last_sync_at"`
	Path       string `json:"path"`
}

type Store struct {
	root string

	mu  sync.Mutex
	dbs map[string]*sql.DB
}

func NewStore(root string) (*Store, error) {
	if root == "" {
		return nil, fmt.Errorf("data root 不能为空")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &Store{root: root, dbs: make(map[string]*sql.DB)}, nil
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var firstErr error
	for k, db := range s.dbs {
		if db == nil {
			continue
		}
		if err := db.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		delete(s.dbs, k)
	}
	return firstErr
}

func (s *Store) db(symbol, timeframe string) (*sql.DB, string, error) {
	if symbol == "" || timeframe == "" {
		return nil, "", fmt.Errorf("symbol/timeframe 不能为空")
	}
	key := strings.ToUpper(symbol) + "@" + strings.ToLower(timeframe)
	s.mu.Lock()
	defer s.mu.Unlock()
	if db, ok := s.dbs[key]; ok && db != nil {
		return db, s.dbPath(symbol, timeframe), nil
	}
	path := s.dbPath(symbol, timeframe)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, "", err
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&cache=shared", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, "", err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := ensureSchema(db, symbol, timeframe); err != nil {
		_ = db.Close()
		return nil, "", err
	}
	s.dbs[key] = db
	return db, path, nil
}

func (s *Store) dbPath(symbol, timeframe string) string {
	dir := filepath.Join(s.root, strings.ToUpper(symbol))
	return filepath.Join(dir, strings.ToLower(timeframe)+".db")
}

// InsertCandles 批量写入 K 线（重复 open_time 将被覆盖）。
func (s *Store) InsertCandles(ctx context.Context, symbol, timeframe string, candles []Candle) (int, error) {
	if len(candles) == 0 {
		return 0, nil
	}
	db, _, err := s.db(symbol, timeframe)
	if err != nil {
		return 0, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO candles (open_time, close_time, open, high, low, close, volume, trades)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(open_time) DO UPDATE SET
		    close_time=excluded.close_time,
		    open=excluded.open,
		    high=excluded.high,
		    low=excluded.low,
		    close=excluded.close,
		    volume=excluded.volume,
		    trades=excluded.trades`)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	defer stmt.Close()
	count := 0
	for _, c := range candles {
		if _, err := stmt.ExecContext(ctx, c.OpenTime, c.CloseTime, c.Open, c.High, c.Low, c.Close, c.Volume, c.Trades); err != nil {
			_ = tx.Rollback()
			return 0, err
		}
		count++
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	if err := s.refreshManifest(ctx, db); err != nil {
		return count, err
	}
	return count, nil
}

// LoadOpenTimes 返回指定区间内已有的 open_time。
func (s *Store) LoadOpenTimes(ctx context.Context, symbol, timeframe string, start, end int64) ([]int64, error) {
	db, _, err := s.db(symbol, timeframe)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT open_time FROM candles WHERE open_time BETWEEN ? AND ? ORDER BY open_time`, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var ts int64
		if err := rows.Scan(&ts); err != nil {
			return nil, err
		}
		out = append(out, ts)
	}
	return out, rows.Err()
}

func (s *Store) Manifest(ctx context.Context, symbol, timeframe string) (Manifest, error) {
	db, path, err := s.db(symbol, timeframe)
	if err != nil {
		return Manifest{}, err
	}
	row := db.QueryRowContext(ctx, `SELECT symbol,timeframe,min_time,max_time,rows,last_sync_at FROM manifest WHERE id=1`)
	var m Manifest
	if err := row.Scan(&m.Symbol, &m.Timeframe, &m.MinTime, &m.MaxTime, &m.Rows, &m.LastSyncAt); err != nil {
		return Manifest{}, err
	}
	m.Path = path
	return m, nil
}

func (s *Store) refreshManifest(ctx context.Context, db *sql.DB) error {
	now := time.Now().UnixMilli()
	_, err := db.ExecContext(ctx, `
		UPDATE manifest
		SET min_time = (SELECT COALESCE(MIN(open_time), 0) FROM candles),
		    max_time = (SELECT COALESCE(MAX(open_time), 0) FROM candles),
		    rows = (SELECT COUNT(1) FROM candles),
		    last_sync_at = ?
		WHERE id = 1`, now)
	return err
}

func ensureSchema(db *sql.DB, symbol, timeframe string) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS candles (
			open_time  INTEGER PRIMARY KEY,
			close_time INTEGER NOT NULL,
			open       REAL NOT NULL,
			high       REAL NOT NULL,
			low        REAL NOT NULL,
			close      REAL NOT NULL,
			volume     REAL NOT NULL,
			trades     INTEGER DEFAULT 0,
			inserted_at INTEGER NOT NULL DEFAULT (strftime('%s','now') * 1000)
		);`,
		`CREATE TABLE IF NOT EXISTS manifest (
			id INTEGER PRIMARY KEY CHECK (id=1),
			symbol TEXT NOT NULL,
			timeframe TEXT NOT NULL,
			min_time INTEGER,
			max_time INTEGER,
			rows INTEGER DEFAULT 0,
			last_sync_at INTEGER
		);`,
		`INSERT INTO manifest (id, symbol, timeframe) VALUES (1, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET symbol=excluded.symbol, timeframe=excluded.timeframe;`,
	}
	for i, stmt := range stmts {
		var err error
		if i == len(stmts)-1 {
			_, err = db.Exec(stmt, strings.ToUpper(symbol), strings.ToLower(timeframe))
		} else {
			_, err = db.Exec(stmt)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// QueryCandles 读取指定区间的 K 线（默认按 open_time 升序返回）。
func (s *Store) QueryCandles(ctx context.Context, symbol, timeframe string, start, end int64, limit int) ([]Candle, error) {
	db, _, err := s.db(symbol, timeframe)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 200
	}
	if limit > 2000 {
		limit = 2000
	}
	orderDesc := false
	var rows *sql.Rows
	switch {
	case start > 0 && end > 0:
		if end < start {
			start, end = end, start
		}
		rows, err = db.QueryContext(ctx, `
			SELECT open_time, close_time, open, high, low, close, volume, trades
			FROM candles WHERE open_time BETWEEN ? AND ?
			ORDER BY open_time ASC LIMIT ?`, start, end, limit)
	case start > 0:
		rows, err = db.QueryContext(ctx, `
			SELECT open_time, close_time, open, high, low, close, volume, trades
			FROM candles WHERE open_time >= ?
			ORDER BY open_time ASC LIMIT ?`, start, limit)
	case end > 0:
		rows, err = db.QueryContext(ctx, `
			SELECT open_time, close_time, open, high, low, close, volume, trades
			FROM candles WHERE open_time <= ?
			ORDER BY open_time DESC LIMIT ?`, end, limit)
		orderDesc = true
	default:
		rows, err = db.QueryContext(ctx, `
			SELECT open_time, close_time, open, high, low, close, volume, trades
			FROM candles ORDER BY open_time DESC LIMIT ?`, limit)
		orderDesc = true
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Candle
	for rows.Next() {
		var c Candle
		if err := rows.Scan(&c.OpenTime, &c.CloseTime, &c.Open, &c.High, &c.Low, &c.Close, &c.Volume, &c.Trades); err != nil {
			return nil, err
		}
		list = append(list, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if orderDesc {
		for i, j := 0, len(list)-1; i < j; i, j = i+1, j-1 {
			list[i], list[j] = list[j], list[i]
		}
	}
	return list, nil
}

// ListAllCandles 返回全部 K 线（按 open_time ASC），仅适合同步数据规模较小场景。
func (s *Store) ListAllCandles(ctx context.Context, symbol, timeframe string) ([]Candle, error) {
	db, _, err := s.db(symbol, timeframe)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `
		SELECT open_time, close_time, open, high, low, close, volume, trades
		FROM candles ORDER BY open_time ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Candle
	for rows.Next() {
		var c Candle
		if err := rows.Scan(&c.OpenTime, &c.CloseTime, &c.Open, &c.High, &c.Low, &c.Close, &c.Volume, &c.Trades); err != nil {
			return nil, err
		}
		list = append(list, c)
	}
	return list, rows.Err()
}

// RangeCandles 返回 start~end 范围内的全部 K 线（开盘时间闭区间）。
func (s *Store) RangeCandles(ctx context.Context, symbol, timeframe string, start, end int64) ([]Candle, error) {
	db, _, err := s.db(symbol, timeframe)
	if err != nil {
		return nil, err
	}
	if start > 0 && end > 0 && end < start {
		start, end = end, start
	}
	if start <= 0 || end <= 0 {
		return nil, fmt.Errorf("start/end 需 > 0")
	}
	rows, err := db.QueryContext(ctx, `
		SELECT open_time, close_time, open, high, low, close, volume, trades
		FROM candles
		WHERE open_time BETWEEN ? AND ?
		ORDER BY open_time ASC`, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Candle
	for rows.Next() {
		var c Candle
		if err := rows.Scan(&c.OpenTime, &c.CloseTime, &c.Open, &c.High, &c.Low, &c.Close, &c.Volume, &c.Trades); err != nil {
			return nil, err
		}
		list = append(list, c)
	}
	return list, rows.Err()
}
