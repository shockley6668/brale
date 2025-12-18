package app

import (
	"context"
	"fmt"
	"strings"

	"brale/internal/agent"
	brcfg "brale/internal/config"
	"brale/internal/logger"
	"brale/internal/market"
	livehttp "brale/internal/transport/http/live"

	"golang.org/x/sync/errgroup"
)

// App 负责应用级编排：加载配置→初始化依赖→启动实时与回测服务。
type App struct {
	cfg        *brcfg.Config
	live       *agent.LiveService
	liveHTTP   *livehttp.Server
	metricsSvc *market.MetricsService // 新增：持有 MetricsService 实例
	Summary    *StartupSummary        // 新增：启动摘要
}

// NewApp 根据配置构建应用对象（不启动）
func NewApp(cfg *brcfg.Config) (*App, error) {
	if cfg == nil {
		return nil, fmt.Errorf("nil config")
	}
	logger.SetLevel(cfg.App.LogLevel)
	return buildAppWithWire(context.Background(), cfg)
}

// Run 启动回测与实时服务。
func (a *App) Run(ctx context.Context) error {
	if a == nil || a.cfg == nil {
		return fmt.Errorf("app not initialized")
	}

	if a.Summary != nil {
		a.Summary.Print()
	}

	if a.live == nil {
		return fmt.Errorf("live service not initialized")
	}
	group, ctx := errgroup.WithContext(ctx)

	if a.liveHTTP != nil {
		group.Go(func() error {
			if err := a.liveHTTP.Start(ctx); err != nil {
				return fmt.Errorf("live http server error: %w", err)
			}
			return nil
		})
	}

	group.Go(func() error {
		defer a.live.Close()
		return a.live.Run(ctx)
	})

	return group.Wait()
}

// LiveService exposes the underlying live service instance (for testing/replay harnesses).
func (a *App) LiveService() *agent.LiveService {
	if a == nil {
		return nil
	}
	return a.live
}

func formatProfileSummary(symbols []string, intervals []string) string {
	toList := func(items []string) string {
		if len(items) == 0 {
			return "-"
		}
		return strings.Join(items, ", ")
	}
	return strings.Join([]string{
		fmt.Sprintf("监控币种数：%d", len(symbols)),
		fmt.Sprintf("- 符号：%s", toList(symbols)),
		fmt.Sprintf("- 订阅周期：%s", toList(intervals)),
	}, "\n")
}
