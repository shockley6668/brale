package app

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"brale/internal/backtest"
	"brale/internal/coins"
	brcfg "brale/internal/config"
	"brale/internal/decision"
	"brale/internal/gateway/database"
	"brale/internal/gateway/notifier"
	"brale/internal/gateway/provider"
	"brale/internal/logger"
	"brale/internal/market"
	brmarket "brale/internal/market"
	"brale/internal/store"
	"brale/internal/strategy"
	backtesthttp "brale/internal/transport/http/backtest"
)

// App 负责应用级编排：加载配置→初始化依赖→启动实时与回测服务。
type App struct {
	cfg      *brcfg.Config
	live     *LiveService
	backtest *BacktestService
}

// NewApp 根据配置构建应用对象（不启动）
func NewApp(cfg *brcfg.Config) (*App, error) {
	if cfg == nil {
		return nil, fmt.Errorf("nil config")
	}
	logger.SetLevel(cfg.App.LogLevel)

	// 符号提供者
	var sp coins.SymbolProvider
	if cfg.Symbols.Provider == "http" {
		sp = coins.NewHTTPSymbolProvider(cfg.Symbols.APIURL)
	} else {
		sp = coins.NewDefaultProvider(cfg.Symbols.DefaultList)
	}

	ctx := context.Background()
	syms, err := sp.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取币种列表失败: %w", err)
	}
	logger.Infof("✓ 已加载 %d 个交易对: %v", len(syms), syms)

	// 选定持仓周期 profile
	horizon, ok := cfg.AI.HoldingProfiles[cfg.AI.ActiveHorizon]
	if !ok {
		return nil, fmt.Errorf("未找到持仓周期配置: %s", cfg.AI.ActiveHorizon)
	}
	hIntervals := horizon.AllTimeframes()
	if len(hIntervals) == 0 {
		return nil, fmt.Errorf("持仓周期 %s 未配置任何 k 线周期", cfg.AI.ActiveHorizon)
	}
	logger.Infof("✓ 启用持仓周期 %s，K线周期=%v", cfg.AI.ActiveHorizon, hIntervals)
	hSummary := formatHorizonSummary(cfg.AI.ActiveHorizon, horizon, hIntervals)
	logger.Infof("[horizon]\n%s", hSummary)

	// 提示词
	pm := strategy.NewManager(cfg.Prompt.Dir)
	if err := pm.Load(); err != nil {
		return nil, fmt.Errorf("加载提示词模板失败: %w", err)
	}
	if content, ok := pm.Get(cfg.Prompt.SystemTemplate); ok {
		logger.Infof("✓ 提示词模板 '%s' 已就绪，长度=%d 字符", cfg.Prompt.SystemTemplate, len(content))
	} else {
		logger.Warnf("未找到提示词模板 '%s'", cfg.Prompt.SystemTemplate)
	}

	// 存储与 WS 更新器
	ks := store.NewMemoryKlineStore()
	updater := brmarket.NewWSUpdater(ks, cfg.Kline.MaxCached)

	// 预热
	lookbacks := horizon.LookbackMap(20)
	preheater := brmarket.NewPreheater(ks, cfg.Kline.MaxCached)
	preheater.Warmup(ctx, syms, lookbacks)
	preheater.Preheat(ctx, syms, hIntervals, cfg.Kline.MaxCached)
	logger.Infof("✓ Warmup 完成，最小条数=%v", lookbacks)
	warmupSummary := fmt.Sprintf("*Warmup 完成*\n```\n%v\n```", lookbacks)

	// 模型 Providers
	var modelCfgs []provider.ModelCfg
	for _, m := range cfg.AI.Models {
		modelCfgs = append(modelCfgs, provider.ModelCfg{ID: m.ID, Provider: m.Provider, Enabled: m.Enabled, APIURL: m.APIURL, APIKey: m.APIKey, Model: m.Model, Headers: m.Headers})
	}
	providers := provider.BuildProvidersFromConfig(modelCfgs)
	if len(providers) == 0 {
		logger.Warnf("未启用任何 AI 模型（请检查 ai.models 配置）")
	} else {
		ids := make([]string, 0, len(providers))
		for _, p := range providers {
			if p != nil && p.Enabled() {
				ids = append(ids, p.ID())
			}
		}
		logger.Infof("✓ 已启用 %d 个 AI 模型: %v", len(ids), ids)
	}

	// 聚合器
	var aggregator decision.Aggregator = decision.FirstWinsAggregator{}
	if cfg.AI.Aggregation == "meta" {
		aggregator = decision.MetaAggregator{Weights: cfg.AI.Weights}
	}

	// 引擎
	engine := &decision.LegacyEngineAdapter{
		Providers:      providers,
		Agg:            aggregator,
		PromptMgr:      pm,
		SystemTemplate: cfg.Prompt.SystemTemplate,
		KStore:         ks,
		Intervals:      hIntervals,
		Horizon:        horizon,
		HorizonName:    cfg.AI.ActiveHorizon,
		Parallel:       true,
		LogEachModel:   cfg.AI.LogEachModel,
		Metrics:        brmarket.NewDefaultMetricsFetcher(""),
		IncludeOI:      true,
		IncludeFunding: true,
		TimeoutSeconds: cfg.MCP.TimeoutSeconds,
	}

	// Telegram（可选）
	var tg *notifier.Telegram
	if cfg.Notify.Telegram.Enabled {
		tg = notifier.NewTelegram(cfg.Notify.Telegram.BotToken, cfg.Notify.Telegram.ChatID)
	}

	var decisionStore *database.DecisionLogStore
	if cfg.AI.DecisionLogPath != "" {
		var err error
		decisionStore, err = database.NewDecisionLogStore(cfg.AI.DecisionLogPath)
		if err != nil {
			return nil, fmt.Errorf("初始化决策日志存储失败: %w", err)
		}
		if obs := database.NewDecisionLogObserver(decisionStore); obs != nil {
			engine.Observer = obs
		}
		logPath := cfg.AI.DecisionLogPath
		if abs, err := filepath.Abs(logPath); err == nil {
			logPath = abs
		}
		logger.Infof("✓ 实盘决策日志写入 %s", logPath)
	}

	includeLastDecision := cfg.AI.IncludeLastDecision
	initialLastJSON := ""
	lastCache := newLastDecisionCache(time.Duration(cfg.AI.LastDecisionMaxAgeSec) * time.Second)
	if !includeLastDecision {
		lastCache = nil
	}
	orderRecorder := market.Recorder(nil)
	if decisionStore != nil {
		orderRecorder = decisionStore
		if lastCache != nil {
			records, err := decisionStore.LoadLastDecisions(ctx)
			if err != nil {
				logger.Warnf("加载 LastDecision 失败: %v", err)
			} else {
				lastCache.Load(records)
				initialLastJSON = flattenLastDecisionJSON(records)
			}
		}
	}

	liveSvc := &LiveService{
		cfg:                 cfg,
		ks:                  ks,
		updater:             updater,
		engine:              engine,
		tg:                  tg,
		decLogs:             decisionStore,
		orderRec:            orderRecorder,
		lastDec:             lastCache,
		includeLastDecision: includeLastDecision,
		symbols:             syms,
		hIntervals:          append([]string(nil), hIntervals...),
		horizonName:         cfg.AI.ActiveHorizon,
		hSummary:            hSummary,
		warmupSummary:       warmupSummary,
		lastOpen:            map[string]time.Time{},
		lastRawJSON:         initialLastJSON,
	}

	var backtestSvc *BacktestService
	if cfg.Backtest.Enabled {
		var err error
		btStore, err := backtest.NewStore(cfg.Backtest.DataDir)
		if err != nil {
			return nil, fmt.Errorf("初始化回测存储失败: %w", err)
		}
		btResults, err := backtest.NewResultStore(cfg.Backtest.DataDir)
		if err != nil {
			btStore.Close()
			return nil, fmt.Errorf("初始化回测结果库失败: %w", err)
		}
		sources := map[string]backtest.CandleSource{
			"binance": backtest.NewBinanceSource(""),
		}
		btSvc, err := backtest.NewService(backtest.ServiceConfig{
			Store:           btStore,
			Sources:         sources,
			DefaultExchange: cfg.Backtest.DefaultExchange,
			RateLimitPerMin: cfg.Backtest.RateLimitPerMin,
			MaxBatch:        cfg.Backtest.MaxBatch,
			MaxConcurrent:   cfg.Backtest.MaxConcurrent,
		})
		if err != nil {
			btResults.Close()
			btStore.Close()
			return nil, fmt.Errorf("初始化回测服务失败: %w", err)
		}
		simFactory := &backtest.AIProxyFactory{
			Prompt:         pm,
			SystemTemplate: cfg.Prompt.SystemTemplate,
			Models:         modelCfgs,
			Aggregator:     aggregator,
			Parallel:       true,
			TimeoutSeconds: cfg.MCP.TimeoutSeconds,
		}
		btSim, err := backtest.NewSimulator(backtest.SimulatorConfig{
			CandleStore:    btStore,
			ResultStore:    btResults,
			Fetcher:        btSvc,
			Profiles:       cfg.AI.HoldingProfiles,
			Lookbacks:      lookbacks,
			DefaultProfile: cfg.AI.ActiveHorizon,
			Strategy:       simFactory,
			Notifier:       tg,
			MaxConcurrent:  cfg.Backtest.MaxConcurrent,
		})
		if err != nil {
			btResults.Close()
			btStore.Close()
			return nil, fmt.Errorf("初始化回测模拟器失败: %w", err)
		}
		btHTTP, err := backtesthttp.NewServer(backtesthttp.Config{
			Addr:      cfg.Backtest.HTTPAddr,
			Svc:       btSvc,
			Simulator: btSim,
			Results:   btResults,
			LiveLogs:  decisionStore,
		})
		if err != nil {
			btResults.Close()
			btStore.Close()
			return nil, fmt.Errorf("初始化回测 HTTP 失败: %w", err)
		}
		logger.Infof("✓ 回测 HTTP 接口监听 %s", cfg.Backtest.HTTPAddr)
		backtestSvc = &BacktestService{
			store:   btStore,
			results: btResults,
			svc:     btSvc,
			sim:     btSim,
			server:  btHTTP,
		}
	}

	return &App{
		cfg:      cfg,
		live:     liveSvc,
		backtest: backtestSvc,
	}, nil
}

// Run 启动回测与实时服务。
func (a *App) Run(ctx context.Context) error {
	if a == nil || a.cfg == nil {
		return fmt.Errorf("app not initialized")
	}
	if a.backtest != nil {
		a.backtest.Start(ctx)
		defer a.backtest.Close()
	}
	if a.live == nil {
		return fmt.Errorf("live service not initialized")
	}
	defer a.live.Close()
	return a.live.Run(ctx)
}

func formatHorizonSummary(name string, profile brcfg.HorizonProfile, intervals []string) string {
	toList := func(items []string) string {
		if len(items) == 0 {
			return "-"
		}
		return strings.Join(items, ", ")
	}
	ind := profile.Indicators
	lines := []string{
		fmt.Sprintf("持仓周期：%s", name),
		fmt.Sprintf("- 入场周期：%s", toList(profile.EntryTimeframes)),
		fmt.Sprintf("- 确认周期：%s", toList(profile.ConfirmTimeframes)),
		fmt.Sprintf("- 背景周期：%s", toList(profile.BackgroundTimeframes)),
		fmt.Sprintf("- 订阅/缓存周期：%s", toList(intervals)),
		fmt.Sprintf("- EMA(fast/mid/slow) = %d / %d / %d", ind.EMA.Fast, ind.EMA.Mid, ind.EMA.Slow),
		fmt.Sprintf("- RSI(period=%d, oversold=%.0f, overbought=%.0f)", ind.RSI.Period, ind.RSI.Oversold, ind.RSI.Overbought),
	}
	return strings.Join(lines, "\n")
}

func flattenLastDecisionJSON(records []decision.LastDecisionRecord) string {
	if len(records) == 0 {
		return ""
	}
	total := 0
	for _, rec := range records {
		total += len(rec.Decisions)
	}
	if total == 0 {
		return ""
	}
	all := make([]decision.Decision, 0, total)
	for _, rec := range records {
		if len(rec.Decisions) == 0 {
			continue
		}
		all = append(all, rec.Decisions...)
	}
	buf, err := json.Marshal(all)
	if err != nil {
		return ""
	}
	return string(buf)
}
