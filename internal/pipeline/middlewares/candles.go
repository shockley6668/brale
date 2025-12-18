package middlewares

import (
	"context"
	"fmt"
	"strings"
	"time"

	"brale/internal/pipeline"
	"brale/internal/store"
)

// CandleFetcherConfig 控制 k 线抓取。
type CandleFetcherConfig struct {
	Name      string
	Stage     int
	Critical  bool
	Timeout   time.Duration
	Intervals []string
	Limit     int
}

// CandleFetcher 将指定周期的 K 线写入 AnalysisContext。
type CandleFetcher struct {
	meta      pipeline.MiddlewareMeta
	exporter  store.SnapshotExporter
	intervals []string
	limit     int
}

// NewCandleFetcher 构造中间件。
func NewCandleFetcher(cfg CandleFetcherConfig, exporter store.SnapshotExporter) *CandleFetcher {
	if cfg.Limit <= 0 {
		cfg.Limit = 240
	}
	return &CandleFetcher{
		meta: pipeline.MiddlewareMeta{
			Name:     nameOrDefault(cfg.Name, "kline_fetcher"),
			Stage:    cfg.Stage,
			Critical: cfg.Critical,
			Timeout:  cfg.Timeout,
		},
		exporter:  exporter,
		intervals: append([]string(nil), cfg.Intervals...),
		limit:     cfg.Limit,
	}
}

// Meta 实现 pipeline.Middleware。
func (c *CandleFetcher) Meta() pipeline.MiddlewareMeta { return c.meta }

// Handle 拉取数据。
func (c *CandleFetcher) Handle(ctx context.Context, ac *pipeline.AnalysisContext) error {
	if c.exporter == nil {
		return fmt.Errorf("kline exporter unavailable")
	}
	if ac == nil {
		return fmt.Errorf("nil analysis context")
	}
	if len(c.intervals) == 0 {
		return fmt.Errorf("no intervals configured")
	}
	for _, iv := range c.intervals {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		candles, err := c.exporter.Export(ctx, ac.Symbol, iv, c.limit)
		if err != nil {
			return fmt.Errorf("export %s %s: %w", ac.Symbol, iv, err)
		}
		if len(candles) == 0 {
			continue
		}
		ac.SetCandles(iv, candles)
	}
	return nil
}

func nameOrDefault(val, fallback string) string {
	if val = strings.TrimSpace(val); val != "" {
		return val
	}
	return fallback
}
