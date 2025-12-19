package trader

type PriceUpdateHandler struct{}

func (h *PriceUpdateHandler) Type() EventType { return EvtPriceUpdate }

func (h *PriceUpdateHandler) Handle(ctx *HandlerContext, payload []byte, traceID string) error {
	return ctx.Trader().handlePriceUpdate(payload)
}

type PositionOpeningHandler struct{}

func (h *PositionOpeningHandler) Type() EventType { return EvtPositionOpening }

func (h *PositionOpeningHandler) Handle(ctx *HandlerContext, payload []byte, traceID string) error {
	return ctx.Trader().applyPositionOpening(payload)
}

type PositionOpenedHandler struct{}

func (h *PositionOpenedHandler) Type() EventType { return EvtPositionOpened }

func (h *PositionOpenedHandler) Handle(ctx *HandlerContext, payload []byte, traceID string) error {
	return ctx.Trader().applyPositionOpened(payload)
}

type PositionClosingHandler struct{}

func (h *PositionClosingHandler) Type() EventType { return EvtPositionClosing }

func (h *PositionClosingHandler) Handle(ctx *HandlerContext, payload []byte, traceID string) error {
	return ctx.Trader().applyPositionClosing(payload)
}

type PositionClosedHandler struct{}

func (h *PositionClosedHandler) Type() EventType { return EvtPositionClosed }

func (h *PositionClosedHandler) Handle(ctx *HandlerContext, payload []byte, traceID string) error {
	return ctx.Trader().applyPositionClosed(payload)
}
