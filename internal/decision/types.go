package decision

// 中文说明：
// 本文件定义 AI 决策相关的通用数据结构，供引擎与聚合器使用。

// ProfileDirective 描述 Profile 对单个 symbol 的附加约束。
type ProfileDirective struct {
	DerivativesEnabled bool
	IncludeOI          bool
	IncludeFunding     bool
	MultiAgentEnabled  bool
}

func (d ProfileDirective) allowDerivatives() bool {
	return d.DerivativesEnabled && (d.IncludeOI || d.IncludeFunding)
}

// Decision 单笔 AI 决策动作（与旧版保持兼容的最小字段集）
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
	// Tiers removed
	ExitPlan *ExitPlanSpec `json:"exit_plan,omitempty"`

	ExitPlanVersion int `json:"-"`
}

// DecisionTiers 描述三段式离场的目标与比例。
// DecisionTiers removed

// UnmarshalJSON removed as it was only for legacy Tiers compatibility.

// DecisionResult AI 决策输出（可包含多币种）
type DecisionResult struct {
	Decisions     []Decision
	RawOutput     string                 // 原始模型完整输出（便于调试/提取思维链）
	RawJSON       string                 // 提取到的 JSON 决策数组文本
	SymbolResults []SymbolDecisionOutput // 拆分按 symbol 的原始输出
	// MetaSummary: 当使用 meta 聚合时，记录各模型投票与理由的汇总文本（用于通知）
	MetaSummary string
	// MetaBreakdown: 当使用 meta 聚合时，记录按币种/动作/模型的投票明细（用于通知与排查分歧）
	MetaBreakdown *MetaVoteBreakdown
	TraceID       string
}

// SymbolDecisionOutput 保存每个币种独立请求的输出摘要。
type SymbolDecisionOutput struct {
	Symbol      string `json:"symbol"`
	RawOutput   string `json:"raw_output"`
	RawJSON     string `json:"raw_json"`
	MetaSummary string `json:"meta_summary"`
	TraceID     string `json:"trace_id"`
}

// DecisionInput is the input for the executor.
type DecisionInput struct {
	TraceID     string
	Decision    Decision
	MarketPrice float64
}
