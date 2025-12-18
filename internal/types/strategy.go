package types

// ExitPlanSpec 描述 AI 返回的退出计划（通用结构，可递归）。
type ExitPlanSpec struct {
	ID         string                 `json:"id"`
	Params     map[string]any         `json:"params"`
	Components []ExitPlanSpec         `json:"components,omitempty"`
	Meta       map[string]any         `json:"meta,omitempty"`
}
