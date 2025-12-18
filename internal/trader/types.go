package trader

import (
	"encoding/json"
	"time"

	"brale/internal/gateway/database"
	"brale/internal/gateway/exchange"
	"brale/internal/strategy"
	"brale/internal/strategy/exit"
)

// SignalEntryPayload carries the order details and the strategy tiers context.
type SignalEntryPayload struct {
	Order exchange.OpenRequest
}

// PositionOpeningPayload carries details when an entry order is accepted but not fully filled.
type PositionOpeningPayload struct {
	TradeID   string    `json:"trade_id"`
	Symbol    string    `json:"symbol"`
	Side      string    `json:"side"`
	Stake     float64   `json:"stake"`
	Leverage  float64   `json:"leverage"`
	Amount    float64   `json:"amount"`
	Price     float64   `json:"price"`
	CreatedAt time.Time `json:"created_at"`
}

// PositionClosingPayload carries info when an exit order is initiated.
type PositionClosingPayload struct {
	TradeID    string    `json:"trade_id"`
	Symbol     string    `json:"symbol"`
	Side       string    `json:"side"`
	CreatedAt  time.Time `json:"created_at"`
	ClosePrice float64   `json:"close_price"`
}

// PositionOpenedPayload carries the details of a newly opened position.
type PositionOpenedPayload struct {
	TradeID  string // Unique ID from Executor
	Symbol   string
	Side     string
	Price    float64
	Amount   float64
	Stake    float64
	Leverage float64
	OpenedAt time.Time
}

// PositionClosedPayload captures the facts for a closed position so it can be
// replayed deterministically from the WAL/event log.
type PositionClosedPayload struct {
	TradeID         string    `json:"trade_id"`
	Symbol          string    `json:"symbol"`
	Side            string    `json:"side"`
	Amount          float64   `json:"amount"`
	RemainingAmount float64   `json:"remaining_amount"`
	ClosePrice      float64   `json:"close_price"`
	Reason          string    `json:"reason"`
	PnL             float64   `json:"pnl"`
	PnLPct          float64   `json:"pnl_pct"`
	ClosedAt        time.Time `json:"closed_at"`
}

// SignalExitPayload carries exit details.
type SignalExitPayload struct {
	TradeID        int // Added for safety
	Symbol         string
	Side           string
	CloseRatio     float64 `json:"close_ratio,omitempty"`
	IsInitialRatio bool    `json:"is_initial_ratio,omitempty"`
}

// PriceUpdatePayload carries market price updates.
type PriceUpdatePayload struct {
	Symbol string
	Quote  strategy.MarketQuote
}

// EventType 定义事件类型
type EventType string

const (
	// EvtPriceUpdate 行情更新
	EvtPriceUpdate EventType = "PRICE_UPDATE"
	// EvtPositionOpening 表示仓位下单但尚未成交
	EvtPositionOpening EventType = "POSITION_OPENING"
	// EvtPositionClosing 表示平仓指令已发出但尚未成交
	EvtPositionClosing EventType = "POSITION_CLOSING"
	// EvtSignalEntry 信号开仓
	EvtSignalEntry EventType = "SIGNAL_ENTRY"
	// EvtSignalExit 信号平仓
	EvtSignalExit EventType = "SIGNAL_EXIT"
	// EvtManualOrder 手动下单
	EvtManualOrder EventType = "MANUAL_ORDER"

	// Domain Events (Facts) - Used for State Replay
	EvtPositionOpened EventType = "POSITION_OPENED"
	EvtPositionClosed EventType = "POSITION_CLOSED"
	// EvtPlanEvent Plan trigger event
	EvtPlanEvent EventType = "PLAN_EVENT"
	// EvtSyncPlans synchronizes strategy plans
	EvtSyncPlans EventType = "SYNC_PLANS"
	// EvtPlanStateUpdate updates a single plan/component snapshot
	EvtPlanStateUpdate EventType = "PLAN_STATE_UPDATE"
	// EvtOrderResult reports async execution result
	EvtOrderResult EventType = "ORDER_RESULT"
)

// SyncPlansPayload carries strategy plans to sync
type SyncPlansPayload struct {
	TradeID int                         `json:"trade_id"`
	Version int64                       `json:"version,omitempty"`
	Plans   []exit.StrategyPlanSnapshot `json:"plans"`
}

// PlanEventPayload carries exit plan event details
type PlanEventPayload struct {
	TradeID   int                    `json:"trade_id"`
	EventType string                 `json:"event_type"`
	Context   map[string]interface{} `json:"context"`
}

// OrderResultPayload carries the result of an async execution
type OrderResultPayload struct {
	RequestID string // Correlates with Command ID (TraceID)
	Action    string // "open" or "close"
	Reason    string // Optional reason for the action
	TradeID   string // Optional existing TradeID
	Symbol    string
	Side      string
	FillPrice float64
	FillSize  float64
	PnL       float64
	PnLPct    float64
	Original  interface{} // The payload that triggered this (e.g. SignalEntryPayload)
	OrderID   string      // Resulting Order/Trade ID from Executor
	Error     string      // If failed
	Timestamp time.Time   `json:"timestamp"`
}

type PlanStateUpdatePayload struct {
	TradeID         int    `json:"trade_id"`
	PlanID          string `json:"plan_id"`
	PlanComponent   string `json:"plan_component"`
	PlanVersion     int    `json:"plan_version"`
	StatusCode      int    `json:"status_code"`
	ParamsJSON      string `json:"params_json"`
	StateJSON       string `json:"state_json"`
	DecisionTraceID string `json:"decision_trace_id,omitempty"`
	Version         int64  `json:"version,omitempty"`
	UpdatedAt       int64  `json:"updated_at,omitempty"`
}

const (
	OrderActionOpen  = "open"
	OrderActionClose = "close"
)

// EventEnvelope 是 Actor 接收的标准消息信封
type EventEnvelope struct {
	ID        string // UUID
	Type      EventType
	Payload   json.RawMessage // 具体的事件数据
	CreatedAt time.Time
	TradeID   int
	Symbol    string

	// ReplyCh 用于同步等待处理结果 (可选)
	ReplyCh chan error `json:"-"`
}

// State 维护 Trader 的内存状态 (无锁，仅单 Goroutine 访问)
type State struct {
	// Positions key: symbol (e.g. "BTC/USDT")
	Positions map[string]*exchange.Position
	// ByTradeID key: TradeID (string) -> Symbol. Used for reverse lookup.
	ByTradeID map[string]string
	// Plans key: TradeID (string) -> plan snapshots.
	Plans map[string][]exit.StrategyPlanSnapshot
	// PlanVersions track last synced version.
	PlanVersions map[string]int64
	// SymbolIndex maps symbol -> TradeID for O(1) reverse lookup.
	SymbolIndex map[string]string
	// TradeEvents caches trade operation logs per trade ID.
	TradeEvents map[string][]database.TradeOperationRecord
}

func NewState() *State {
	return &State{
		Positions:    make(map[string]*exchange.Position),
		ByTradeID:    make(map[string]string),
		Plans:        make(map[string][]exit.StrategyPlanSnapshot),
		PlanVersions: make(map[string]int64),
		SymbolIndex:  make(map[string]string),
		TradeEvents:  make(map[string][]database.TradeOperationRecord),
	}
}

// TradeIDBySymbol performs a reverse lookup to find the trade ID given a
// symbol. Opposed to storing another map we keep this helper to avoid data
// divergence until a dedicated index lands.
func (s *State) TradeIDBySymbol(symbol string) string {
	if s == nil || s.SymbolIndex == nil {
		return ""
	}
	return s.SymbolIndex[symbol]
}
