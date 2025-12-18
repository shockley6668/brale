package decision

import "context"

// AgentOutputHistory allows the decision engine to query the latest output of a specific agent
// (strictly matched by symbol + stage + provider_id) from the previous rounds.
type AgentOutputHistory interface {
	LatestAgentOutput(ctx context.Context, symbol, stage, providerID string) (string, error)
}

