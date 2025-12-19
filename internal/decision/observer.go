package decision

import "context"

// DecisionObserver receives decision results for logging/notification.
// Called after each decision cycle with full trace data.
type DecisionObserver interface {
	AfterDecide(ctx context.Context, trace DecisionTrace)
}

// DecisionTrace captures everything about a decision for debugging.
// Stored in decision_logs table, displayed in admin dashboard.
type DecisionTrace struct {
	TraceID       string
	SystemPrompt  string
	UserPrompt    string
	Outputs       []ModelOutput // All provider responses (for comparison)
	Best          ModelOutput   // Final aggregated result
	Candidates    []string      // Symbols analyzed
	Timeframes    []string      // Kline intervals used
	HorizonName   string        // Active profile group
	Positions     []PositionSnapshot
	AgentInsights []AgentInsight // Multi-agent intermediate reasoning
}
