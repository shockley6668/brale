package trader

// EventHandler processes a specific event type in the trader actor loop.
// Implementations: SignalEntryHandler, PositionOpeningHandler, ExitFillHandler, etc.
type EventHandler interface {
	Type() EventType
	// Payload is JSON-encoded event data. TraceID links to decision log.
	Handle(ctx *HandlerContext, payload []byte, traceID string) error
}

// HandlerContext provides access to Trader internals.
// Passed to handlers for state mutation and side effects.
type HandlerContext struct {
	trader *Trader
}

func NewHandlerContext(t *Trader) *HandlerContext {
	return &HandlerContext{trader: t}
}

func (c *HandlerContext) Trader() *Trader {
	return c.trader
}
