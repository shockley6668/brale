package freqtrade

import (
	"context"
	"time"

	"brale/internal/strategy/exit"
)

type APIPosition struct {
	TradeID            int                    `json:"trade_id"`
	Symbol             string                 `json:"symbol"`
	Side               string                 `json:"side"`
	EntryPrice         float64                `json:"entry_price"`
	Amount             float64                `json:"amount"`
	InitialAmount      float64                `json:"initial_amount,omitempty"`
	Stake              float64                `json:"stake"`
	Leverage           float64                `json:"leverage"`
	PositionValue      float64                `json:"position_value,omitempty"`
	OpenedAt           int64                  `json:"opened_at"`
	HoldingMs          int64                  `json:"holding_ms"`
	StopLoss           float64                `json:"stop_loss,omitempty"`
	TakeProfit         float64                `json:"take_profit,omitempty"`
	CurrentPrice       float64                `json:"current_price,omitempty"`
	PnLRatio           float64                `json:"pnl_ratio,omitempty"`
	PnLUSD             float64                `json:"pnl_usd,omitempty"`
	UnrealizedPnLRatio float64                `json:"unrealized_pnl_ratio,omitempty"`
	UnrealizedPnLUSD   float64                `json:"unrealized_pnl_usd,omitempty"`
	RemainingRatio     float64                `json:"remaining_ratio,omitempty"`
	Placeholder        bool                   `json:"placeholder,omitempty"`
	Events             []TradeEvent           `json:"events,omitempty"`
	PlanSummaries      []string               `json:"plan_summaries,omitempty"`
	Plans              []StrategyPlanSnapshot `json:"plans,omitempty"`
	Status             string                 `json:"status"`
	ClosedAt           int64                  `json:"closed_at,omitempty"`
	ExitPrice          float64                `json:"exit_price,omitempty"`
	ExitReason         string                 `json:"exit_reason,omitempty"`
}

type TradeEvent struct {
	FreqtradeID int            `json:"freqtrade_id"`
	Symbol      string         `json:"symbol"`
	Operation   int            `json:"operation"`
	Details     map[string]any `json:"details,omitempty"`
	Timestamp   time.Time      `json:"timestamp"`
}

type StrategyPlanSnapshot = exit.StrategyPlanSnapshot

type PositionListOptions struct {
	Symbol      string
	Page        int
	PageSize    int
	IncludeLogs bool
	LogsLimit   int
}

type PositionListResult struct {
	TotalCount int           `json:"total_count"`
	Page       int           `json:"page"`
	PageSize   int           `json:"page_size"`
	Positions  []APIPosition `json:"positions"`
}

type PlanUpdateHook interface {
	NotifyPlanUpdated(ctx context.Context, tradeID int)
}
