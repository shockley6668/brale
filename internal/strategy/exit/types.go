package exit

type StrategyPlanSnapshot struct {
	PlanID          string `json:"plan_id"`
	PlanComponent   string `json:"plan_component"`
	PlanVersion     int    `json:"plan_version"`
	StatusLabel     string `json:"status"`
	StatusCode      int    `json:"status_code"`
	ParamsJSON      string `json:"params_json"`
	StateJSON       string `json:"state_json"`
	DecisionTraceID string `json:"decision_trace_id,omitempty"`
	CreatedAt       int64  `json:"created_at,omitempty"`
	UpdatedAt       int64  `json:"updated_at,omitempty"`
}
