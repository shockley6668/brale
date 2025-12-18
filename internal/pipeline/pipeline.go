package pipeline

import (
	"context"
	"fmt"
	"sort"

	"brale/internal/logger"

	"golang.org/x/sync/errgroup"
)

// Pipeline 负责按 stage 调度一组中间件。
type Pipeline struct {
	name   string
	stages [][]Middleware
}

// New 创建 Pipeline，并按 stage 归类中间件。
func New(name string, middlewares ...Middleware) *Pipeline {
	if len(middlewares) == 0 {
		return &Pipeline{name: name, stages: nil}
	}
	stageMap := make(map[int][]Middleware)
	for _, mw := range middlewares {
		if mw == nil {
			continue
		}
		meta := mw.Meta()
		stageMap[meta.Stage] = append(stageMap[meta.Stage], mw)
	}
	keys := make([]int, 0, len(stageMap))
	for st := range stageMap {
		keys = append(keys, st)
	}
	sort.Ints(keys)
	stages := make([][]Middleware, 0, len(keys))
	for _, st := range keys {
		stages = append(stages, stageMap[st])
	}
	return &Pipeline{name: name, stages: stages}
}

// Run 执行 pipeline。
func (p *Pipeline) Run(ctx context.Context, ac *AnalysisContext) error {
	if ac == nil {
		return fmt.Errorf("nil analysis context")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for _, stage := range p.stages {
		if err := p.runStage(ctx, ac, stage); err != nil {
			return err
		}
	}
	return nil
}

func (p *Pipeline) runStage(ctx context.Context, ac *AnalysisContext, stage []Middleware) error {
	if len(stage) == 0 {
		return nil
	}
	group, stageCtx := errgroup.WithContext(ctx)
	warnCh := make(chan *MiddlewareError, len(stage))
	for _, mw := range stage {
		mw := mw
		if mw == nil {
			continue
		}
		group.Go(func() error {
			meta := mw.Meta()
			runCtx := stageCtx
			var cancel context.CancelFunc
			if meta.Timeout > 0 {
				runCtx, cancel = context.WithTimeout(stageCtx, meta.Timeout)
				defer cancel()
			}
			err := mw.Handle(runCtx, ac)
			if err == nil {
				return nil
			}
			wErr := &MiddlewareError{
				Middleware: meta.Name,
				Stage:      meta.Stage,
				Critical:   meta.Critical,
				Err:        err,
			}
			if meta.Critical {
				return wErr
			}
			select {
			case warnCh <- wErr:
			default:
			}
			return nil
		})
	}
	err := group.Wait()
	close(warnCh)
	for warn := range warnCh {
		if warn == nil {
			continue
		}
		ac.AddWarning(warn.Error())
		logger.Warnf("[pipeline] %s %s", p.name, warn.Error())
	}
	if err == nil {
		return nil
	}
	ac.AddWarning(err.Error())
	return err
}
