package pipeline

import (
	"context"
	"time"
)

// Middleware 描述一个特征抽取步骤。
type Middleware interface {
	Meta() MiddlewareMeta
	Handle(ctx context.Context, ac *AnalysisContext) error
}

// MiddlewareMeta 提供调度所需元信息。
type MiddlewareMeta struct {
	Name     string
	Stage    int
	Critical bool
	Timeout  time.Duration
}

// MiddlewareError 封装中间件的失败信息。
type MiddlewareError struct {
	Middleware string
	Stage      int
	Critical   bool
	Err        error
}

func (e *MiddlewareError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return e.Middleware
	}
	return e.Middleware + ": " + e.Err.Error()
}

func (e *MiddlewareError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}
