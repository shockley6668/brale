package exit

import (
	"fmt"
	"strings"
	"sync"
)

// HandlerRegistry 维护所有注册的 PlanHandler。
type HandlerRegistry struct {
	mu       sync.RWMutex
	handlers map[string]PlanHandler
}

// NewHandlerRegistry 构造空 registry。
func NewHandlerRegistry() *HandlerRegistry {
	return &HandlerRegistry{
		handlers: make(map[string]PlanHandler),
	}
}

// Register 将 handler 放入 registry，若 ID 重复则覆盖。
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

// Handler 返回指定 ID 对应的 PlanHandler。
func (r *HandlerRegistry) Handler(id string) (PlanHandler, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.handlers[strings.TrimSpace(id)]
	return h, ok
}

// MustHandler 获取 handler，若不存在则 panic。
func (r *HandlerRegistry) MustHandler(id string) PlanHandler {
	if h, ok := r.Handler(id); ok {
		return h
	}
	panic(fmt.Sprintf("exit handler 未注册: %s", id))
}

// Handlers 返回所有已注册 handler（只读切片）。
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
