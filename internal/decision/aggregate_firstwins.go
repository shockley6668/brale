package decision

import (
    "context"
    "errors"
)

// FirstWinsAggregator 取第一个成功的输出
type FirstWinsAggregator struct{}

func (a FirstWinsAggregator) Name() string { return "first-wins" }

func (a FirstWinsAggregator) Aggregate(ctx context.Context, outputs []ModelOutput) (ModelOutput, error) {
    for _, o := range outputs {
        if o.Err == nil && len(o.Parsed.Decisions) > 0 {
            return o, nil
        }
    }
    return ModelOutput{}, errors.New("无可用的模型输出")
}

