package decision

import (
	"context"

	"brale/internal/gateway/provider"
)

// ModelOutput wraps a single LLM provider's response.
// Multiple outputs are collected in parallel, then aggregated.
type ModelOutput struct {
	ProviderID    string // e.g. "deepseek", "qwen", "doubao"
	SystemPrompt  string
	UserPrompt    string
	Raw           string                  // Raw LLM response text
	Parsed        DecisionResult          // Parsed decisions array
	Err           error                   // Parse or API error
	Images        []provider.ImagePayload // Vision inputs (chart screenshots)
	VisionEnabled bool
	ImageCount    int
}

// Aggregator combines outputs from multiple LLM providers into a final decision.
// Implementations: FirstWinsAggregator (first success), MetaAggregator (weighted vote).
type Aggregator interface {
	Aggregate(ctx context.Context, outputs []ModelOutput) (ModelOutput, error)
	Name() string
}
