package trader

type SignalEntryHandler struct{}

func (h *SignalEntryHandler) Type() EventType { return EvtSignalEntry }

func (h *SignalEntryHandler) Handle(ctx *HandlerContext, payload []byte, traceID string) error {
	return ctx.Trader().handleSignalEntry(payload, traceID)
}

type SignalExitHandler struct{}

func (h *SignalExitHandler) Type() EventType { return EvtSignalExit }

func (h *SignalExitHandler) Handle(ctx *HandlerContext, payload []byte, traceID string) error {
	return ctx.Trader().handleSignalExit(payload, traceID)
}

type OrderResultHandler struct{}

func (h *OrderResultHandler) Type() EventType { return EvtOrderResult }

func (h *OrderResultHandler) Handle(ctx *HandlerContext, payload []byte, traceID string) error {
	return ctx.Trader().handleOrderResult(payload)
}
