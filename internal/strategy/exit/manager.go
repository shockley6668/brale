package exit

import (
	"fmt"
	"strings"
	"sync"
)

type HandlerRegistry struct {
	mu       sync.RWMutex
	handlers map[string]PlanHandler
}

func NewHandlerRegistry() *HandlerRegistry {
	return &HandlerRegistry{
		handlers: make(map[string]PlanHandler),
	}
}

func (r *HandlerRegistry) Register(h PlanHandler) {
	if r == nil || h == nil {
		return
	}
	id := strings.TrimSpace(h.ID())
	if id == "" {
		panic("exit handler 注册失败: ID 不能为空")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[id] = h
}

func (r *HandlerRegistry) Handler(id string) (PlanHandler, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.handlers[strings.TrimSpace(id)]
	return h, ok
}

func (r *HandlerRegistry) MustHandler(id string) PlanHandler {
	if h, ok := r.Handler(id); ok {
		return h
	}
	panic(fmt.Sprintf("exit handler 未注册: %s", id))
}

func (r *HandlerRegistry) Handlers() []PlanHandler {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	list := make([]PlanHandler, 0, len(r.handlers))
	for _, h := range r.handlers {
		list = append(list, h)
	}
	return list
}
