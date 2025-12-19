package types

import (
	"time"
)

type PositionSnapshot struct {
	Symbol          string   `json:"symbol"`
	Side            string   `json:"side"`
	EntryPrice      float64  `json:"entry_price"`
	Quantity        float64  `json:"quantity"`
	Stake           float64  `json:"stake,omitempty"`
	Leverage        float64  `json:"leverage,omitempty"`
	TakeProfit      float64  `json:"take_profit"`
	StopLoss        float64  `json:"stop_loss"`
	CurrentPrice    float64  `json:"current_price,omitempty"`
	UnrealizedPn    float64  `json:"unrealized_pn"`
	UnrealizedPnPct float64  `json:"unrealized_pn_pct"`
	PositionValue   float64  `json:"position_value,omitempty"`
	AccountRatio    float64  `json:"account_ratio,omitempty"`
	RR              float64  `json:"rr"`
	HoldingMs       int64    `json:"holding_ms"`
	RemainingRatio  float64  `json:"remaining_ratio,omitempty"`
	PlanSummaries   []string `json:"plan_summaries,omitempty"`
	PlanStateJSON   string   `json:"plan_state_json,omitempty"`
}

type AccountSnapshot struct {
	Total     float64   `json:"total"`
	Available float64   `json:"available"`
	Used      float64   `json:"used"`
	Currency  string    `json:"currency"`
	UpdatedAt time.Time `json:"updated_at"`
}
