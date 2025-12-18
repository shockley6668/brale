package trader

// PriceUpdateHandler handles EvtPriceUpdate events.
type PriceUpdateHandler struct{}

func (h *PriceUpdateHandler) Type() EventType { return EvtPriceUpdate }

func (h *PriceUpdateHandler) Handle(ctx *HandlerContext, payload []byte, traceID string) error {
	return ctx.Trader().handlePriceUpdate(payload)
}

// PositionOpeningHandler handles EvtPositionOpening events.
type PositionOpeningHandler struct{}

func (h *PositionOpeningHandler) Type() EventType { return EvtPositionOpening }

func (h *PositionOpeningHandler) Handle(ctx *HandlerContext, payload []byte, traceID string) error {
	return ctx.Trader().applyPositionOpening(payload)
}

// PositionOpenedHandler handles EvtPositionOpened events.
type PositionOpenedHandler struct{}

func (h *PositionOpenedHandler) Type() EventType { return EvtPositionOpened }

func (h *PositionOpenedHandler) Handle(ctx *HandlerContext, payload []byte, traceID string) error {
	return ctx.Trader().applyPositionOpened(payload)
}

// PositionClosingHandler handles EvtPositionClosing events.
type PositionClosingHandler struct{}

func (h *PositionClosingHandler) Type() EventType { return EvtPositionClosing }

func (h *PositionClosingHandler) Handle(ctx *HandlerContext, payload []byte, traceID string) error {
	return ctx.Trader().applyPositionClosing(payload)
}

// PositionClosedHandler handles EvtPositionClosed events.
type PositionClosedHandler struct{}

func (h *PositionClosedHandler) Type() EventType { return EvtPositionClosed }

func (h *PositionClosedHandler) Handle(ctx *HandlerContext, payload []byte, traceID string) error {
	return ctx.Trader().applyPositionClosed(payload)
}
