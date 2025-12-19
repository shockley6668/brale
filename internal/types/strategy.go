package types

type ExitPlanSpec struct {
	ID         string         `json:"id"`
	Params     map[string]any `json:"params"`
	Components []ExitPlanSpec `json:"components,omitempty"`
	Meta       map[string]any `json:"meta,omitempty"`
}
