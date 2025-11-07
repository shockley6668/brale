package backtest

import (
	"context"

	"brale/internal/ai"
	brcfg "brale/internal/config"
)

// AILogRecord 记录单次模型输出（含输入提示词）。
type AILogRecord struct {
	ProviderID   string
	Stage        string
	SystemPrompt string
	UserPrompt   string
	RawOutput    string
	RawJSON      string
	MetaSummary  string
	Decisions    []ai.Decision
	Error        string
	Note         string
}

// Strategy 用于根据给定的 K 线快照生成 AI 决策。
type Strategy interface {
	Decide(ctx context.Context, req StrategyRequest) (ai.DecisionResult, error)
	Logs() []AILogRecord
	Close() error
}

// StrategyFactory 按 run 级别创建策略实例（可携带模型/状态）。
type StrategyFactory interface {
	NewStrategy(spec StrategySpec) (Strategy, error)
}

// StrategySpec 表示一次 run 的上下文。
type StrategySpec struct {
	RunID       string
	Symbol      string
	ProfileName string
	Profile     brcfg.HorizonProfile
}

// StrategyRequest 包含某一时刻的多周期 K 线数据。
type StrategyRequest struct {
	RunID       string
	Symbol      string
	ProfileName string
	Profile     brcfg.HorizonProfile
	CurrentTime int64
	Timeframes  map[string][]Candle
	Positions   []StrategyPosition
}

// StrategyPosition 提供给 AI 的仓位信息。
type StrategyPosition struct {
	Symbol          string
	Side            string
	EntryPrice      float64
	Quantity        float64
	TakeProfit      float64
	StopLoss        float64
	UnrealizedPn    float64
	UnrealizedPnPct float64
	RR              float64
	HoldingMs       int64
}
