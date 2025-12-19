package provider

import "context"

type ImagePayload struct {
	DataURI     string
	Description string
}

type ChatPayload struct {
	System     string
	User       string
	Images     []ImagePayload
	ExpectJSON bool
	MaxTokens  int
}

type ModelProvider interface {
	ID() string
	Enabled() bool
	SupportsVision() bool
	ExpectsJSON() bool

	Call(ctx context.Context, payload ChatPayload) (string, error)
}
