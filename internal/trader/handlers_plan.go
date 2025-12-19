package trader

type PlanEventHandler struct{}

func (h *PlanEventHandler) Type() EventType { return EvtPlanEvent }

func (h *PlanEventHandler) Handle(ctx *HandlerContext, payload []byte, traceID string) error {
	return ctx.Trader().handlePlanEvent(payload)
}

type PlanStateUpdateHandler struct{}

func (h *PlanStateUpdateHandler) Type() EventType { return EvtPlanStateUpdate }

func (h *PlanStateUpdateHandler) Handle(ctx *HandlerContext, payload []byte, traceID string) error {
	return ctx.Trader().handlePlanStateUpdate(payload)
}

type SyncPlansHandler struct{}

func (h *SyncPlansHandler) Type() EventType { return EvtSyncPlans }

func (h *SyncPlansHandler) Handle(ctx *HandlerContext, payload []byte, traceID string) error {
	return ctx.Trader().handleSyncPlans(payload)
}
