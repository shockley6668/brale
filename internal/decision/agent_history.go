package decision

import "context"

type AgentOutputSnapshot struct {
	Output      string
	Fingerprint string
	Timestamp   int64
}

type AgentOutputHistory interface {
	LatestAgentOutput(ctx context.Context, symbol, stage, providerID string) (AgentOutputSnapshot, error)
}
