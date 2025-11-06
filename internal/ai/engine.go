package ai

// 中文说明：
// 引擎输入数据类型与提示词载体。
// 当前仅保留给 LegacyEngineAdapter 使用，不再暴露/依赖通用引擎接口。

import "context"

// Context 引擎输入上下文（简化版）：可扩展为账户/持仓/候选币/指标等
type Context struct {
    Candidates []string              // 候选币种
    Market     map[string]MarketData // 各币种聚合指标（可后续填充）
    Prompt     PromptBundle          // System/User 提示词
}

// MarketData 占位结构：后续可接 K 线指标/OI/Funding 等
type MarketData struct{}

// PromptBundle 引擎使用的提示词材料
type PromptBundle struct {
    System string
    User   string
}

// Decider 决策器接口：将上下文转为决策结果
type Decider interface {
    Decide(ctx context.Context, input Context) (DecisionResult, error)
}
