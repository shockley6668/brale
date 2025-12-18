package trader

// EventHandler defines the interface for handling specific event types.
// Each implementation handles one event type and encapsulates the processing logic.
type EventHandler interface {
	// Type returns the event type this handler processes.
	Type() EventType

	// Handle processes the event and returns an error if processing failed.
	// The traceID can be used for logging and tracing.
	Handle(ctx *HandlerContext, payload []byte, traceID string) error
}

// HandlerContext provides access to Trader internals for handlers.
// This avoids exposing the entire Trader struct while giving handlers
// the access they need.
type HandlerContext struct {
	trader *Trader
}

// NewHandlerContext creates a new handler context wrapping a Trader.
func NewHandlerContext(t *Trader) *HandlerContext {
	return &HandlerContext{trader: t}
}

// Trader returns the underlying Trader instance.
// Handlers should use this sparingly and prefer specific accessor methods.
func (c *HandlerContext) Trader() *Trader {
	return c.trader
}
