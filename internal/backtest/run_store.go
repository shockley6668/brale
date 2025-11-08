package backtest

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"brale/internal/decision"

	_ "modernc.org/sqlite"
)

// ResultStore 管理 backtest_runs/order/position/snapshot 表。
type ResultStore struct {
	mu   sync.Mutex
	db   *sql.DB
	path string
}

func NewResultStore(root string) (*ResultStore, error) {
	if root == "" {
		return nil, fmt.Errorf("result store root 不能为空")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(root, "runs.db")
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&cache=shared", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := ensureResultSchema(db); err != nil {
		db.Close()
		return nil, err
	}
	return &ResultStore{db: db, path: path}, nil
}

func (s *ResultStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	return err
}

func ensureResultSchema(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS backtest_runs (
			id TEXT PRIMARY KEY,
			symbol TEXT NOT NULL,
			profile TEXT NOT NULL,
			status TEXT NOT NULL,
			start_ts INTEGER NOT NULL,
			end_ts INTEGER NOT NULL,
			execution_tf TEXT NOT NULL,
			initial_balance REAL NOT NULL,
			final_balance REAL NOT NULL DEFAULT 0,
			profit REAL NOT NULL DEFAULT 0,
			return_pct REAL NOT NULL DEFAULT 0,
			win_rate REAL NOT NULL DEFAULT 0,
			max_drawdown REAL NOT NULL DEFAULT 0,
			orders INTEGER NOT NULL DEFAULT 0,
			positions INTEGER NOT NULL DEFAULT 0,
			config_json TEXT NOT NULL,
			stats_json TEXT,
			message TEXT,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			completed_at INTEGER
		);`,
		`CREATE TABLE IF NOT EXISTS backtest_orders (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			run_id TEXT NOT NULL,
			action TEXT NOT NULL,
			side TEXT NOT NULL,
			type TEXT NOT NULL,
			price REAL NOT NULL,
			quantity REAL NOT NULL,
			notional REAL NOT NULL,
			fee REAL NOT NULL,
			timeframe TEXT NOT NULL,
			decided_at INTEGER NOT NULL,
			executed_at INTEGER NOT NULL,
			decision_json TEXT,
			meta_json TEXT,
			take_profit REAL,
			stop_loss REAL,
			expected_rr REAL,
			FOREIGN KEY(run_id) REFERENCES backtest_runs(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS backtest_positions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			run_id TEXT NOT NULL,
			symbol TEXT NOT NULL,
			side TEXT NOT NULL,
			entry_order_id INTEGER,
			exit_order_id INTEGER,
			entry_price REAL,
			exit_price REAL,
			quantity REAL,
			pnl REAL,
			pnl_pct REAL,
			holding_ms INTEGER,
			opened_at INTEGER,
			closed_at INTEGER,
			take_profit REAL,
			stop_loss REAL,
			expected_rr REAL,
			FOREIGN KEY(run_id) REFERENCES backtest_runs(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS backtest_snapshots (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			run_id TEXT NOT NULL,
			ts INTEGER NOT NULL,
			equity REAL NOT NULL,
			balance REAL NOT NULL,
			drawdown REAL NOT NULL,
			exposure REAL NOT NULL,
			note TEXT,
			FOREIGN KEY(run_id) REFERENCES backtest_runs(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS backtest_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			run_id TEXT NOT NULL,
			candle_ts INTEGER NOT NULL,
			timeframe TEXT NOT NULL,
			provider_id TEXT,
			stage TEXT,
			system_prompt TEXT,
			user_prompt TEXT,
			raw_output TEXT,
			raw_json TEXT,
			meta_summary TEXT,
			decisions_json TEXT,
			error TEXT,
			note TEXT,
			created_at INTEGER NOT NULL,
			FOREIGN KEY(run_id) REFERENCES backtest_runs(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_orders_run ON backtest_orders(run_id);`,
		`CREATE INDEX IF NOT EXISTS idx_positions_run ON backtest_positions(run_id);`,
		`CREATE INDEX IF NOT EXISTS idx_snapshots_run ON backtest_snapshots(run_id);`,
		`CREATE INDEX IF NOT EXISTS idx_logs_run ON backtest_logs(run_id, candle_ts);`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	if err := ensureLogColumns(db); err != nil {
		return err
	}
	if err := ensureOrderColumns(db); err != nil {
		return err
	}
	return nil
}

// InsertRun 写入一条 run 记录。
func (s *ResultStore) InsertRun(ctx context.Context, run Run) error {
	cfgJSON, err := json.Marshal(run.Config)
	if err != nil {
		return err
	}
	statsJSON, err := json.Marshal(run.Stats)
	if err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO backtest_runs
			(id, symbol, profile, status, start_ts, end_ts, execution_tf, initial_balance,
			final_balance, profit, return_pct, win_rate, max_drawdown, orders, positions,
			config_json, stats_json, message, created_at, updated_at, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID, run.Symbol, run.Profile, run.Status, run.StartTS, run.EndTS, run.ExecutionTimeframe,
		run.InitialBalance, run.FinalBalance, run.Stats.Profit, run.Stats.ReturnPct, run.Stats.WinRate,
		run.Stats.MaxDrawdownPct, run.Orders, run.Positions, string(cfgJSON), bytesOrNil(statsJSON),
		run.Message, now, now, nullableTime(run.CompletedAt))
	return err
}

func bytesOrNil(b []byte) interface{} {
	if len(b) == 0 {
		return nil
	}
	return string(b)
}

func nullableTime(t time.Time) interface{} {
	if t.IsZero() {
		return nil
	}
	return t.UnixMilli()
}

// UpdateRunSummary 更新状态、指标。
func (s *ResultStore) UpdateRunSummary(ctx context.Context, id string, status string, stats RunStats, message string) error {
	statsJSON, err := json.Marshal(stats)
	if err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	var completed interface{}
	if status == RunStatusDone || status == RunStatusFailed {
		completed = now
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE backtest_runs
		SET status=?, final_balance=?, profit=?, return_pct=?, win_rate=?, max_drawdown=?,
		    orders=?, positions=?, stats_json=?, message=?, updated_at=?,
		    completed_at=CASE WHEN ? IS NULL THEN completed_at ELSE ? END
		WHERE id=?`,
		status, stats.FinalBalance, stats.Profit, stats.ReturnPct, stats.WinRate,
		stats.MaxDrawdownPct, stats.Orders, stats.Positions, string(statsJSON), message, now,
		completed, completed, id)
	return err
}

// UpdateRunStatus 仅更新状态与提示。
func (s *ResultStore) UpdateRunStatus(ctx context.Context, id, status, message string) error {
	now := time.Now().UnixMilli()
	var completed interface{}
	if status == RunStatusDone || status == RunStatusFailed {
		completed = now
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE backtest_runs
		SET status=?, message=?, updated_at=?, completed_at=CASE WHEN ? IS NULL THEN completed_at ELSE ? END
		WHERE id=?`, status, message, now, completed, completed, id)
	return err
}

func (s *ResultStore) InsertOrder(ctx context.Context, order *Order) (int64, error) {
	if order == nil {
		return 0, fmt.Errorf("order 不能为空")
	}
	var decisionJSON interface{}
	if len(order.Decision) > 0 {
		decisionJSON = string(order.Decision)
	}
	var metaJSON interface{}
	if len(order.Meta) > 0 {
		metaJSON = string(order.Meta)
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO backtest_orders
			(run_id, action, side, type, price, quantity, notional, fee, timeframe,
			 decided_at, executed_at, decision_json, meta_json, take_profit, stop_loss, expected_rr)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		order.RunID, order.Action, order.Side, order.Type, order.Price, order.Quantity,
		order.Notional, order.Fee, order.Timeframe, order.DecidedAt.UnixMilli(), order.ExecutedAt.UnixMilli(),
		decisionJSON, metaJSON, nullIfZero(order.TakeProfit), nullIfZero(order.StopLoss), nullIfZero(order.ExpectedRR))
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err == nil {
		order.ID = id
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE backtest_runs SET orders=orders+1 WHERE id=?`, order.RunID)
	return id, err
}

func (s *ResultStore) InsertPosition(ctx context.Context, pos *Position) (int64, error) {
	if pos == nil {
		return 0, fmt.Errorf("position 不能为空")
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO backtest_positions
			(run_id, symbol, side, entry_order_id, exit_order_id, entry_price, exit_price,
			 quantity, pnl, pnl_pct, holding_ms, opened_at, closed_at, take_profit, stop_loss, expected_rr)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		pos.RunID, pos.Symbol, pos.Side, pos.EntryOrderID, pos.ExitOrderID, pos.EntryPrice,
		pos.ExitPrice, pos.Quantity, pos.PnL, pos.PnLPct, pos.HoldingMs,
		pos.OpenedAt.UnixMilli(), pos.ClosedAt.UnixMilli(), nullIfZero(pos.TakeProfit),
		nullIfZero(pos.StopLoss), nullIfZero(pos.ExpectedRR))
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err == nil {
		pos.ID = id
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE backtest_runs SET positions=positions+1 WHERE id=?`, pos.RunID)
	return id, err
}

func (s *ResultStore) InsertSnapshot(ctx context.Context, snap Snapshot) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO backtest_snapshots
			(run_id, ts, equity, balance, drawdown, exposure, note)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		snap.RunID, snap.TS, snap.Equity, snap.Balance, snap.Drawdown, snap.Exposure, snap.Note)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// RunLog 记录一次 AI 决策调用的完整结果。
type RunLog struct {
	ID           int64               `json:"id"`
	RunID        string              `json:"run_id"`
	CandleTS     int64               `json:"candle_ts"`
	Timeframe    string              `json:"timeframe"`
	ProviderID   string              `json:"provider_id"`
	Stage        string              `json:"stage"`
	SystemPrompt string              `json:"system_prompt"`
	UserPrompt   string              `json:"user_prompt"`
	RawOutput    string              `json:"raw_output"`
	RawJSON      string              `json:"raw_json"`
	MetaSummary  string              `json:"meta_summary"`
	Decisions    []decision.Decision `json:"decisions"`
	Error        string              `json:"error"`
	Note         string              `json:"note"`
	CreatedAt    time.Time           `json:"created_at"`
}

func (s *ResultStore) InsertRunLog(ctx context.Context, log RunLog) (int64, error) {
	decisionsJSON, err := json.Marshal(log.Decisions)
	if err != nil {
		return 0, err
	}
	now := time.Now().UnixMilli()
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO backtest_logs
			(run_id, candle_ts, timeframe, provider_id, stage, system_prompt, user_prompt,
			 raw_output, raw_json, meta_summary, decisions_json, error, note, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		log.RunID, log.CandleTS, log.Timeframe, log.ProviderID, log.Stage, log.SystemPrompt, log.UserPrompt,
		log.RawOutput, log.RawJSON, log.MetaSummary, string(decisionsJSON), log.Error, log.Note, now)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err == nil {
		log.ID = id
	}
	return id, err
}

func (s *ResultStore) ListRunLogs(ctx context.Context, runID string, limit int) ([]RunLog, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, candle_ts, timeframe, provider_id, stage, system_prompt, user_prompt,
		       raw_output, raw_json, meta_summary, decisions_json, error, note, created_at
		FROM backtest_logs
		WHERE run_id=?
		ORDER BY candle_ts ASC, id ASC
		LIMIT ?`, runID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RunLog
	for rows.Next() {
		var log RunLog
		var created int64
		var decisionsStr sql.NullString
		if err := rows.Scan(&log.ID, &log.CandleTS, &log.Timeframe, &log.ProviderID, &log.Stage, &log.SystemPrompt, &log.UserPrompt,
			&log.RawOutput, &log.RawJSON, &log.MetaSummary, &decisionsStr, &log.Error, &log.Note, &created); err != nil {
			return nil, err
		}
		log.RunID = runID
		log.CreatedAt = timeFromMillis(created)
		if decisionsStr.Valid && decisionsStr.String != "" {
			if err := json.Unmarshal([]byte(decisionsStr.String), &log.Decisions); err != nil {
				return nil, err
			}
		}
		out = append(out, log)
	}
	return out, rows.Err()
}

func (s *ResultStore) ListRuns(ctx context.Context, limit int) ([]Run, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, symbol, profile, status, start_ts, end_ts, execution_tf, initial_balance,
		       final_balance, profit, return_pct, win_rate, max_drawdown, orders, positions,
		       config_json, stats_json, message, created_at, updated_at, completed_at
		FROM backtest_runs
		ORDER BY created_at DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Run
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, run)
	}
	return list, rows.Err()
}

func (s *ResultStore) GetRun(ctx context.Context, id string) (Run, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, symbol, profile, status, start_ts, end_ts, execution_tf, initial_balance,
		       final_balance, profit, return_pct, win_rate, max_drawdown, orders, positions,
		       config_json, stats_json, message, created_at, updated_at, completed_at
		FROM backtest_runs WHERE id=?`, id)
	return scanRun(row)
}

func (s *ResultStore) ListOrders(ctx context.Context, runID string, limit int) ([]Order, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, action, side, type, price, quantity, notional, fee, timeframe,
		       decided_at, executed_at, decision_json, meta_json, take_profit, stop_loss, expected_rr
		FROM backtest_orders
		WHERE run_id=?
		ORDER BY id ASC
		LIMIT ?`, runID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Order
	for rows.Next() {
		var ord Order
		var decisionStr sql.NullString
		var metaStr sql.NullString
		var decidedAt, executedAt int64
		var tp, sl, rr sql.NullFloat64
		if err := rows.Scan(&ord.ID, &ord.Action, &ord.Side, &ord.Type, &ord.Price,
			&ord.Quantity, &ord.Notional, &ord.Fee, &ord.Timeframe, &decidedAt, &executedAt,
			&decisionStr, &metaStr, &tp, &sl, &rr); err != nil {
			return nil, err
		}
		ord.RunID = runID
		ord.DecidedAt = timeFromMillis(decidedAt)
		ord.ExecutedAt = timeFromMillis(executedAt)
		if decisionStr.Valid {
			ord.Decision = json.RawMessage(decisionStr.String)
		}
		if metaStr.Valid {
			ord.Meta = json.RawMessage(metaStr.String)
		}
		if tp.Valid {
			ord.TakeProfit = tp.Float64
		}
		if sl.Valid {
			ord.StopLoss = sl.Float64
		}
		if rr.Valid {
			ord.ExpectedRR = rr.Float64
		}
		out = append(out, ord)
	}
	return out, rows.Err()
}

func (s *ResultStore) ListPositions(ctx context.Context, runID string, limit int) ([]Position, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, symbol, side, entry_order_id, exit_order_id, entry_price, exit_price,
		       quantity, pnl, pnl_pct, holding_ms, opened_at, closed_at,
		       take_profit, stop_loss, expected_rr
		FROM backtest_positions
		WHERE run_id=?
		ORDER BY id ASC
		LIMIT ?`, runID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Position
	for rows.Next() {
		var pos Position
		var openedAt, closedAt int64
		var tp, sl, rr sql.NullFloat64
		if err := rows.Scan(&pos.ID, &pos.Symbol, &pos.Side, &pos.EntryOrderID, &pos.ExitOrderID,
			&pos.EntryPrice, &pos.ExitPrice, &pos.Quantity, &pos.PnL, &pos.PnLPct,
			&pos.HoldingMs, &openedAt, &closedAt, &tp, &sl, &rr); err != nil {
			return nil, err
		}
		pos.RunID = runID
		pos.OpenedAt = timeFromMillis(openedAt)
		pos.ClosedAt = timeFromMillis(closedAt)
		if tp.Valid {
			pos.TakeProfit = tp.Float64
		}
		if sl.Valid {
			pos.StopLoss = sl.Float64
		}
		if rr.Valid {
			pos.ExpectedRR = rr.Float64
		}
		out = append(out, pos)
	}
	return out, rows.Err()
}

func (s *ResultStore) ListSnapshots(ctx context.Context, runID string, limit int) ([]Snapshot, error) {
	if limit <= 0 || limit > 2000 {
		limit = 400
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, ts, equity, balance, drawdown, exposure, note
		FROM backtest_snapshots
		WHERE run_id=?
		ORDER BY ts ASC
		LIMIT ?`, runID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Snapshot
	for rows.Next() {
		var snap Snapshot
		if err := rows.Scan(&snap.ID, &snap.TS, &snap.Equity, &snap.Balance, &snap.Drawdown, &snap.Exposure, &snap.Note); err != nil {
			return nil, err
		}
		snap.RunID = runID
		out = append(out, snap)
	}
	return out, rows.Err()
}

func ensureLogColumns(db *sql.DB) error {
	columns := []struct {
		name string
		typ  string
	}{
		{"provider_id", "TEXT"},
		{"stage", "TEXT"},
		{"system_prompt", "TEXT"},
		{"user_prompt", "TEXT"},
		{"error", "TEXT"},
		{"note", "TEXT"},
	}
	for _, col := range columns {
		if err := addColumnIfMissing(db, "backtest_logs", col.name, col.typ); err != nil {
			return err
		}
	}
	return nil
}

func ensureOrderColumns(db *sql.DB) error {
	columns := []struct {
		table string
		name  string
		typ   string
	}{
		{"backtest_orders", "take_profit", "REAL"},
		{"backtest_orders", "stop_loss", "REAL"},
		{"backtest_orders", "expected_rr", "REAL"},
		{"backtest_positions", "take_profit", "REAL"},
		{"backtest_positions", "stop_loss", "REAL"},
		{"backtest_positions", "expected_rr", "REAL"},
	}
	for _, col := range columns {
		if err := addColumnIfMissing(db, col.table, col.name, col.typ); err != nil {
			return err
		}
	}
	return nil
}

func addColumnIfMissing(db *sql.DB, table, column, typ string) error {
	exists, err := columnExists(db, table, column)
	if err != nil || exists {
		return err
	}
	stmt := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, typ)
	_, err = db.Exec(stmt)
	return err
}

func columnExists(db *sql.DB, table, column string) (bool, error) {
	query := fmt.Sprintf("SELECT COUNT(1) FROM pragma_table_info('%s') WHERE name='%s'", table, column)
	var cnt int
	if err := db.QueryRow(query).Scan(&cnt); err != nil {
		return false, err
	}
	return cnt > 0, nil
}

func nullIfZero(v float64) interface{} {
	if v == 0 {
		return nil
	}
	return v
}

type scanner interface {
	Scan(dest ...interface{}) error
}

func scanRun(row scanner) (Run, error) {
	var run Run
	var cfgStr string
	var statsStr sql.NullString
	var createdAt, updatedAt int64
	var completedAt sql.NullInt64
	if err := row.Scan(&run.ID, &run.Symbol, &run.Profile, &run.Status,
		&run.StartTS, &run.EndTS, &run.ExecutionTimeframe, &run.InitialBalance,
		&run.FinalBalance, &run.Profit, &run.ReturnPct, &run.WinRate, &run.MaxDrawdownPct,
		&run.Orders, &run.Positions, &cfgStr, &statsStr, &run.Message, &createdAt, &updatedAt, &completedAt); err != nil {
		return Run{}, err
	}
	run.CreatedAt = timeFromMillis(createdAt)
	run.UpdatedAt = timeFromMillis(updatedAt)
	if completedAt.Valid {
		run.CompletedAt = timeFromMillis(completedAt.Int64)
	}
	if err := json.Unmarshal([]byte(cfgStr), &run.Config); err != nil {
		return Run{}, err
	}
	if statsStr.Valid && statsStr.String != "" {
		if err := json.Unmarshal([]byte(statsStr.String), &run.Stats); err != nil {
			return Run{}, err
		}
	} else {
		run.Stats = RunStats{
			FinalBalance:   run.FinalBalance,
			Profit:         run.Profit,
			ReturnPct:      run.ReturnPct,
			WinRate:        run.WinRate,
			MaxDrawdownPct: run.MaxDrawdownPct,
			Orders:         run.Orders,
			Positions:      run.Positions,
		}
	}
	return run, nil
}

func timeFromMillis(ms int64) time.Time {
	if ms <= 0 {
		return time.Time{}
	}
	return time.Unix(0, ms*int64(time.Millisecond))
}