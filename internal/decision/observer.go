package decision

import "context"

// DecisionObserver 在每次模型聚合后回调，便于外部记录输入/输出。
type DecisionObserver interface {
	AfterDecide(ctx context.Context, trace DecisionTrace)
}

// DecisionTrace 描述一次完整调用的材料与结果。
type DecisionTrace struct {
	SystemPrompt string
	UserPrompt   string
	Outputs      []ModelOutput
	Best         ModelOutput
	Candidates   []string
	Timeframes   []string
	HorizonName  string
	Positions    []PositionSnapshot
}
