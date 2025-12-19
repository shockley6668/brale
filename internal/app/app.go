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

type App struct {
	cfg        *brcfg.Config
	live       *agent.LiveService
	liveHTTP   *livehttp.Server
	metricsSvc *market.MetricsService
	Summary    *StartupSummary
}

func NewApp(cfg *brcfg.Config) (*App, error) {
	if cfg == nil {
		return nil, fmt.Errorf("nil config")
	}
	logger.SetLevel(cfg.App.LogLevel)
	return buildAppWithWire(context.Background(), cfg)
}

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
