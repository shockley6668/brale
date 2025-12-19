package decision

import "context"

type AgentOutputHistory interface {
	LatestAgentOutput(ctx context.Context, symbol, stage, providerID string) (string, error)
}
