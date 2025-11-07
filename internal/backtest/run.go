package backtest

import (
	"encoding/json"
	"time"
)

const (
	RunStatusPending = "pending"
	RunStatusRunning = "running"
	RunStatusDone    = "done"
	RunStatusFailed  = "failed"
)

// RunConfig 记录本次模拟的参数快照，便于重放。
type RunConfig struct {
	Profile            string   `json:"profile"`
	Symbol             string   `json:"symbol"`
	StartTS            int64    `json:"start_ts"`
	EndTS              int64    `json:"end_ts"`
	ExecutionTimeframe string   `json:"execution_timeframe"`
	EntryTimeframes    []string `json:"entry_timeframes"`
	ConfirmTimeframes  []string `json:"confirm_timeframes"`
	BackgroundTFs      []string `json:"background_timeframes"`
	InitialBalance     float64  `json:"initial_balance"`
	FeeRate            float64  `json:"fee_rate"`
	SlippageBps        float64  `json:"slippage_bps"`
	Leverage           int      `json:"leverage"`
	Notes              string   `json:"notes,omitempty"`
}

// RunStats 汇总收益、风控指标，供前端展示。
type RunStats struct {
	FinalBalance      float64   `json:"final_balance"`
	Profit            float64   `json:"profit"`
	ReturnPct         float64   `json:"return_pct"`
	WinRate           float64   `json:"win_rate"`
	MaxDrawdownPct    float64   `json:"max_drawdown_pct"`
	Orders            int       `json:"orders"`
	Positions         int       `json:"positions"`
	Wins              int       `json:"wins"`
	Losses            int       `json:"losses"`
	AvgHoldingMinutes float64   `json:"avg_holding_minutes"`
	Snapshots         int       `json:"snapshots"`
	EquityPeak        float64   `json:"equity_peak"`
	EquityValley      float64   `json:"equity_valley"`
	Notes             []string  `json:"notes,omitempty"`
	FinishedAt        time.Time `json:"finished_at"`
}

// Run 表示一次模拟任务。
type Run struct {
	ID                 string    `json:"id"`
	Symbol             string    `json:"symbol"`
	Profile            string    `json:"profile"`
	Status             string    `json:"status"`
	StartTS            int64     `json:"start_ts"`
	EndTS              int64     `json:"end_ts"`
	ExecutionTimeframe string    `json:"execution_timeframe"`
	InitialBalance     float64   `json:"initial_balance"`
	FinalBalance       float64   `json:"final_balance"`
	Profit             float64   `json:"profit"`
	ReturnPct          float64   `json:"return_pct"`
	WinRate            float64   `json:"win_rate"`
	MaxDrawdownPct     float64   `json:"max_drawdown_pct"`
	Message            string    `json:"message"`
	Config             RunConfig `json:"config"`
	Stats              RunStats  `json:"stats"`
	Orders             int       `json:"orders"`
	Positions          int       `json:"positions"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	CompletedAt        time.Time `json:"completed_at"`
}

// MarshalStats 返回 stats JSON。
func (r Run) MarshalStats() ([]byte, error) {
	return json.Marshal(r.Stats)
}

// MarshalConfig 返回 config JSON。
func (r Run) MarshalConfig() ([]byte, error) {
	return json.Marshal(r.Config)
}

// Order 记录一次模拟下单行为（开仓/平仓）。
type Order struct {
	ID         int64           `json:"id"`
	RunID      string          `json:"run_id"`
	Action     string          `json:"action"` // open_long/close_long/open_short/close_short
	Side       string          `json:"side"`   // long/short
	Type       string          `json:"type"`   // market/limit（目前仅支持市价）
	Price      float64         `json:"price"`
	Quantity   float64         `json:"quantity"`
	Notional   float64         `json:"notional"`
	Fee        float64         `json:"fee"`
	Timeframe  string          `json:"timeframe"`
	DecidedAt  time.Time       `json:"decided_at"`
	ExecutedAt time.Time       `json:"executed_at"`
	TakeProfit float64         `json:"take_profit,omitempty"`
	StopLoss   float64         `json:"stop_loss,omitempty"`
	ExpectedRR float64         `json:"expected_rr,omitempty"`
	Decision   json.RawMessage `json:"decision,omitempty"`
	Meta       json.RawMessage `json:"meta,omitempty"`
}

// Position 记录一次完整持仓的盈亏。
type Position struct {
	ID           int64     `json:"id"`
	RunID        string    `json:"run_id"`
	Symbol       string    `json:"symbol"`
	Side         string    `json:"side"`
	EntryOrderID int64     `json:"entry_order_id"`
	ExitOrderID  int64     `json:"exit_order_id"`
	EntryPrice   float64   `json:"entry_price"`
	ExitPrice    float64   `json:"exit_price"`
	Quantity     float64   `json:"quantity"`
	PnL          float64   `json:"pnl"`
	PnLPct       float64   `json:"pnl_pct"`
	HoldingMs    int64     `json:"holding_ms"`
	TakeProfit   float64   `json:"take_profit,omitempty"`
	StopLoss     float64   `json:"stop_loss,omitempty"`
	ExpectedRR   float64   `json:"expected_rr,omitempty"`
	OpenedAt     time.Time `json:"opened_at"`
	ClosedAt     time.Time `json:"closed_at"`
}

// Snapshot 保存资金曲线。
type Snapshot struct {
	ID       int64   `json:"id"`
	RunID    string  `json:"run_id"`
	TS       int64   `json:"ts"`
	Equity   float64 `json:"equity"`
	Balance  float64 `json:"balance"`
	Drawdown float64 `json:"drawdown"`
	Exposure float64 `json:"exposure"`
	Note     string  `json:"note,omitempty"`
}

// RunRequest 为 HTTP 提交使用。
type RunRequest struct {
	Symbol             string  `json:"symbol" binding:"required"`
	Profile            string  `json:"profile"`
	StartTS            int64   `json:"start_ts" binding:"required"`
	EndTS              int64   `json:"end_ts" binding:"required"`
	ExecutionTimeframe string  `json:"execution_timeframe"`
	InitialBalance     float64 `json:"initial_balance"`
	FeeRate            float64 `json:"fee_rate"`
	SlippageBps        float64 `json:"slippage_bps"`
	Leverage           int     `json:"leverage"`
}
