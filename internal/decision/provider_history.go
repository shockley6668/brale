package decision

import "context"

type ProviderOutputSnapshot struct {
	ProviderID string
	Decisions  []Decision
	RawOutput  string
	Timestamp  int64
}

type ProviderOutputHistory interface {
	LatestProviderOutputs(ctx context.Context, symbol string) ([]ProviderOutputSnapshot, error)
}
