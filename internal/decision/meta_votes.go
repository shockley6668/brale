package decision

// MetaVoteBreakdown records meta aggregation vote details so notification can render
// per-symbol disagreements clearly (instead of relying on parsing MetaSummary text).
type MetaVoteBreakdown struct {
	Threshold        float64               `json:"threshold"`
	TotalWeight      float64               `json:"total_weight"`
	GlobalHoldWeight float64               `json:"global_hold_weight,omitempty"`
	Symbols          []MetaSymbolBreakdown `json:"symbols,omitempty"`
}

type MetaSymbolBreakdown struct {
	Symbol      string                `json:"symbol"`
	FinalAction string                `json:"final_action,omitempty"` // 该币种最终执行的动作（如 "HOLD"、"open_long"）
	Votes       []MetaActionVote      `json:"votes,omitempty"`
	Providers   []MetaProviderActions `json:"providers,omitempty"`
}

type MetaActionVote struct {
	Action string  `json:"action"`
	Weight float64 `json:"weight"`
}

type MetaProviderActions struct {
	ProviderID string   `json:"provider_id"`
	Weight     float64  `json:"weight,omitempty"`
	Actions    []string `json:"actions,omitempty"`
}
