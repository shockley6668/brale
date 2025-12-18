package trader

// SignalEntryHandler handles EvtSignalEntry events.
type SignalEntryHandler struct{}

func (h *SignalEntryHandler) Type() EventType { return EvtSignalEntry }

func (h *SignalEntryHandler) Handle(ctx *HandlerContext, payload []byte, traceID string) error {
	return ctx.Trader().handleSignalEntry(payload, traceID)
}

// SignalExitHandler handles EvtSignalExit events.
type SignalExitHandler struct{}

func (h *SignalExitHandler) Type() EventType { return EvtSignalExit }

func (h *SignalExitHandler) Handle(ctx *HandlerContext, payload []byte, traceID string) error {
	return ctx.Trader().handleSignalExit(payload, traceID)
}

// OrderResultHandler handles EvtOrderResult events.
type OrderResultHandler struct{}

func (h *OrderResultHandler) Type() EventType { return EvtOrderResult }

func (h *OrderResultHandler) Handle(ctx *HandlerContext, payload []byte, traceID string) error {
	return ctx.Trader().handleOrderResult(payload)
}
