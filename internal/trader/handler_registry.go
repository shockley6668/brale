package trader

import "brale/internal/logger"

// HandlerRegistry manages event handlers and dispatches events to them.
type HandlerRegistry struct {
	handlers map[EventType]EventHandler
}

// NewHandlerRegistry creates a new registry.
func NewHandlerRegistry() *HandlerRegistry {
	return &HandlerRegistry{
		handlers: make(map[EventType]EventHandler),
	}
}

// Register adds a handler to the registry.
// If a handler for the same event type already exists, it will be replaced.
func (r *HandlerRegistry) Register(h EventHandler) {
	if h == nil {
		return
	}
	r.handlers[h.Type()] = h
}

// Get returns the handler for the given event type.
func (r *HandlerRegistry) Get(t EventType) (EventHandler, bool) {
	h, ok := r.handlers[t]
	return h, ok
}

// RegisterDefaultHandlers registers all built-in event handlers.
func (r *HandlerRegistry) RegisterDefaultHandlers() {
	r.Register(&PriceUpdateHandler{})
	r.Register(&PositionOpeningHandler{})
	r.Register(&PositionOpenedHandler{})
	r.Register(&PositionClosingHandler{})
	r.Register(&PositionClosedHandler{})
	r.Register(&SignalEntryHandler{})
	r.Register(&SignalExitHandler{})
	r.Register(&PlanEventHandler{})
	r.Register(&PlanStateUpdateHandler{})
	r.Register(&SyncPlansHandler{})
	r.Register(&OrderResultHandler{})
	logger.Debugf("Trader: Registered %d event handlers", len(r.handlers))
}
