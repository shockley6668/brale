package decision

import "context"

// 中文说明：
// 聚合相关基础类型与接口。

// ModelOutput 模型执行后的统一表示
type ModelOutput struct {
    ProviderID string
    Raw        string
    Parsed     DecisionResult
    Err        error
}

// Aggregator 聚合接口
type Aggregator interface {
    Aggregate(ctx context.Context, outputs []ModelOutput) (ModelOutput, error)
    Name() string
}

