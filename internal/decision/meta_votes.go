package decision

type MetaVoteBreakdown struct {
	Threshold        float64               `json:"threshold"`
	TotalWeight      float64               `json:"total_weight"`
	GlobalHoldWeight float64               `json:"global_hold_weight,omitempty"`
	Symbols          []MetaSymbolBreakdown `json:"symbols,omitempty"`
}

type MetaSymbolBreakdown struct {
	Symbol      string                `json:"symbol"`
	FinalAction string                `json:"final_action,omitempty"`
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
