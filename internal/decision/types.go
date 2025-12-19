package decision

type ProfileDirective struct {
	DerivativesEnabled bool
	IncludeOI          bool
	IncludeFunding     bool
	MultiAgentEnabled  bool
}

func (d ProfileDirective) allowDerivatives() bool {
	return d.DerivativesEnabled && (d.IncludeOI || d.IncludeFunding)
}

type Decision struct {
	Symbol          string  `json:"symbol"`
	Action          string  `json:"action"`
	ContextTag      string  `json:"context_tag,omitempty"`
	Profile         string  `json:"profile,omitempty"`
	Leverage        int     `json:"leverage,omitempty"`
	PositionSizeUSD float64 `json:"position_size_usd,omitempty"`
	CloseRatio      float64 `json:"close_ratio,omitempty"`
	StopLoss        float64 `json:"stop_loss,omitempty"`
	TakeProfit      float64 `json:"take_profit,omitempty"`
	Confidence      int     `json:"confidence,omitempty"`
	Reasoning       string  `json:"reasoning,omitempty"`

	ExitPlan *ExitPlanSpec `json:"exit_plan,omitempty"`

	ExitPlanVersion int `json:"-"`
}

type DecisionResult struct {
	Decisions     []Decision
	RawOutput     string
	RawJSON       string
	SymbolResults []SymbolDecisionOutput

	MetaSummary string

	MetaBreakdown *MetaVoteBreakdown
	TraceID       string
}

type SymbolDecisionOutput struct {
	Symbol      string `json:"symbol"`
	RawOutput   string `json:"raw_output"`
	RawJSON     string `json:"raw_json"`
	MetaSummary string `json:"meta_summary"`
	TraceID     string `json:"trace_id"`
}

type DecisionInput struct {
	TraceID     string
	Decision    Decision
	MarketPrice float64
}
