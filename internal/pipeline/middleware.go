package pipeline

import (
	"context"
	"time"
)

type Middleware interface {
	Meta() MiddlewareMeta
	Handle(ctx context.Context, ac *AnalysisContext) error
}

type MiddlewareMeta struct {
	Name     string
	Stage    int
	Critical bool
	Timeout  time.Duration
}

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
