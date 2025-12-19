package trader

import "brale/internal/logger"

type HandlerRegistry struct {
	handlers map[EventType]EventHandler
}

func NewHandlerRegistry() *HandlerRegistry {
	return &HandlerRegistry{
		handlers: make(map[EventType]EventHandler),
	}
}

func (r *HandlerRegistry) Register(h EventHandler) {
	if h == nil {
		return
	}
	r.handlers[h.Type()] = h
}

func (r *HandlerRegistry) Get(t EventType) (EventHandler, bool) {
	h, ok := r.handlers[t]
	return h, ok
}

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
