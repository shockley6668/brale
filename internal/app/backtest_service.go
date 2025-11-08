package app

import (
	"context"

	"brale/internal/backtest"
	"brale/internal/logger"
	backtesthttp "brale/internal/transport/http/backtest"
)

// BacktestService 管理回测数据、服务与 HTTP 暴露。
type BacktestService struct {
	store   *backtest.Store
	results *backtest.ResultStore
	svc     *backtest.Service
	sim     *backtest.Simulator
	server  *backtesthttp.Server
}

// Start 绑定上下文并在需要时启动 HTTP 服务。
func (b *BacktestService) Start(ctx context.Context) {
	if b == nil {
		return
	}
	if b.svc != nil {
		b.svc.SetContext(ctx)
	}
	if b.sim != nil {
		b.sim.SetContext(ctx)
	}
	if b.server != nil {
		go func() {
			if err := b.server.Start(ctx); err != nil {
				logger.Warnf("回测 HTTP 停止: %v", err)
			}
		}()
	}
}

// Close 释放回测相关资源。
func (b *BacktestService) Close() {
	if b == nil {
		return
	}
	if b.results != nil {
		_ = b.results.Close()
	}
	if b.store != nil {
		_ = b.store.Close()
	}
}
