package config

import (
	"fmt"
	"strings"
)

const (
	// 应用运行环境 (dev/prod)
	// 默认: "dev"
	// 重置: app.env
	defaultAppEnv = "dev"
	// 日志级别 (debug/info/warn/error)
	// 默认: "info"
	// 重置: app.log_level
	defaultAppLogLevel = "info"
	// HTTP 服务监听地址
	// 默认: ":9991"
	// 重置: app.http_addr
	defaultAppHTTPAddr = ":9991"
	// 应用主日志文件路径
	// 默认: "/data/logs/brale-live.log"
	// 重置: app.log_path
	defaultAppLogPath = "/data/logs/brale-live.log"
	// LLM 交互日志文件路径
	// 默认: "/data/logs/brale-llm.log"
	// 重置: app.llm_log_path
	defaultAppLLMLogPath = "/data/logs/brale-llm.log"

	// K线数据最大缓存数量
	// 默认: 300
	// 重置: kline.max_cached
	defaultKlineMaxCached = 300

	// 默认市场交易所名称
	// 默认: "binance"
	// 重置: market.sources[0].name (当配置为空时)
	defaultMarketName = "binance"
	// 默认市场 REST API 地址
	// 默认: "https://fapi.binance.com"
	// 重置: market.sources[0].rest_base_url (当配置为空时)
	defaultMarketREST = "https://fapi.binance.com"

	// AI 决策聚合策略 (meta/first)
	// 默认: "meta" (多模型投票)
	// 重置: ai.aggregation
	defaultAIAggregation = "meta"
	// AI 决策日志数据库路径
	// 默认: "/data/live/decisions.db"
	// 重置: ai.decision_log_path
	defaultAIDecisionLog = "/data/live/decisions.db"
	// 决策执行偏移时间（秒），防止整点并发
	// 默认: 10
	// 重置: ai.decision_offset_seconds
	defaultAIDecisionOffset = 10

	// MCP 服务超时时间（秒）
	// 默认: 300
	// 重置: mcp.timeout_seconds
	defaultMCPTimeout = 300

	// Freqtrade API 地址
	// 默认: "http://freqtrade:8080/api/v1"
	// 重置: freqtrade.api_url
	defaultFreqtradeAPI = "http://freqtrade:8080/api/v1"
	// Freqtrade 默认每单金额 (USD)
	// 默认: 100
	// 重置: freqtrade.default_stake_usd
	defaultFreqtradeStake = 100
	// Freqtrade 默认杠杆倍数
	// 默认: 1
	// 重置: freqtrade.default_leverage
	defaultFreqtradeLev = 1
	// Freqtrade 请求超时时间（秒）
	// 默认: 15
	// 重置: freqtrade.timeout_seconds
	defaultFreqtradeTimeout = 15
	// Brale 接收 Freqtrade Webhook 的地址
	// 默认: "http://brale:9991/api/live/freqtrade/webhook"
	// 重置: freqtrade.webhook_url
	defaultFreqtradeWebhook = "http://brale:9991/api/live/freqtrade/webhook"
	// Freqtrade 风险数据库路径
	// 默认: "/data/db/trade_risk.db"
	// 重置: freqtrade.risk_store_path
	defaultFreqtradeRiskDB = "/data/db/trade_risk.db"

	// 高级配置：最小流动性过滤 (百万 USD)
	// 默认: 15
	// 重置: advanced.liquidity_filter_usd_m
	defaultAdvancedLiquidity = 15
	// 高级配置：最小盈亏比要求
	// 默认: 1 (R:R >= 1:1)
	// 重置: advanced.min_risk_reward
	defaultAdvancedRR = 1
	// 高级配置：开仓冷却时间（秒）
	// 默认: 180 (3分钟)
	// 重置: advanced.open_cooldown_seconds
	defaultAdvancedCooldown = 180
	// 高级配置：每个周期最大开仓数量
	// 默认: 3
	// 重置: advanced.max_opens_per_cycle
	defaultAdvancedMaxOpen = 3
	// 高级配置：退出计划刷新间隔（秒）
	// 默认: 5
	// 重置: advanced.plan_refresh_interval_seconds
	defaultAdvancedPlanRefresh = 5

	// 交易模式 (static/dynamic)
	// 默认: "static"
	// 重置: trading.mode
	defaultTradingMode = "static"
	// 最大单仓位资产占比 (0.01 = 1%)
	// 默认: 0.01
	// 重置: trading.max_position_pct
	defaultTradingMaxPct = 0.01
	// 默认交易杠杆
	// 默认: 10
	// 重置: trading.default_leverage
	defaultTradingLeverage = 10

	// 币种 Profile 配置文件路径
	// 默认: "configs/profiles.yaml"
	// 重置: ai.profiles_path
	defaultProfilesPath = "configs/profiles.yaml"
	// 退出策略配置文件路径
	// 默认: "configs/exit_strategies.yaml"
	// 重置: ai.exit_strategies_path
	defaultExitPlanPath = "configs/exit_strategies.yaml"
)

func (c *Config) applyDefaults(keys keySet) {
	c.App.applyDefaults(keys)
	c.Kline.applyDefaults(keys)
	c.Prompt.applyDefaults(keys)
	c.MCP.applyDefaults(keys)
	c.Market.applyDefaults(keys)
	c.AI.applyDefaults(keys)
	c.Freqtrade.applyDefaults(keys)
	c.Advanced.applyDefaults(keys)
	c.Trading.applyDefaults(keys)
}

func (a *AppConfig) applyDefaults(keys keySet) {
	if a == nil {
		return
	}
	applyFieldDefaults(keys,
		stringFieldDefault("app.env", &a.Env, defaultAppEnv),
		stringFieldDefault("app.log_level", &a.LogLevel, defaultAppLogLevel),
		stringFieldDefault("app.http_addr", &a.HTTPAddr, defaultAppHTTPAddr),
		stringFieldDefault("app.log_path", &a.LogPath, defaultAppLogPath),
		stringFieldDefault("app.llm_log_path", &a.LLMLog, defaultAppLLMLogPath),
	)
}

func (k *KlineConfig) applyDefaults(keys keySet) {
	if k == nil {
		return
	}
	applyFieldDefaults(keys,
		fieldDefault{
			key:   "kline.max_cached",
			need:  func() bool { return k.MaxCached <= 0 },
			apply: func() { k.MaxCached = defaultKlineMaxCached },
		},
	)
}

func (m *MCPConfig) applyDefaults(keys keySet) {
	if m == nil {
		return
	}
	applyFieldDefaults(keys,
		fieldDefault{
			key:   "mcp.timeout_seconds",
			need:  func() bool { return m.TimeoutSeconds <= 0 },
			apply: func() { m.TimeoutSeconds = defaultMCPTimeout },
		},
	)
}

func (p *PromptConfig) applyDefaults(keys keySet) {
	if p == nil {
		return
	}
	applyFieldDefaults(keys,
		stringFieldDefault("prompt.dir", &p.Dir, "prompts"),
		stringFieldDefault("prompt.system_template", &p.SystemTemplate, "default"),
	)
}

func (a *AdvancedConfig) applyDefaults(keys keySet) {
	if a == nil {
		return
	}
	applyFieldDefaults(keys,
		fieldDefault{
			key:   "advanced.liquidity_filter_usd_m",
			need:  func() bool { return a.LiquidityFilterUSDM <= 0 },
			apply: func() { a.LiquidityFilterUSDM = defaultAdvancedLiquidity },
		},
		fieldDefault{
			key:   "advanced.min_risk_reward",
			need:  func() bool { return a.MinRiskReward <= 0 },
			apply: func() { a.MinRiskReward = defaultAdvancedRR },
		},
		fieldDefault{
			key:   "advanced.open_cooldown_seconds",
			need:  func() bool { return a.OpenCooldownSeconds <= 0 },
			apply: func() { a.OpenCooldownSeconds = defaultAdvancedCooldown },
		},
		fieldDefault{
			key:   "advanced.max_opens_per_cycle",
			need:  func() bool { return a.MaxOpensPerCycle <= 0 },
			apply: func() { a.MaxOpensPerCycle = defaultAdvancedMaxOpen },
		},
		fieldDefault{
			key:   "advanced.plan_refresh_interval_seconds",
			need:  func() bool { return a.PlanRefreshIntervalSeconds <= 0 },
			apply: func() { a.PlanRefreshIntervalSeconds = defaultAdvancedPlanRefresh },
		},
	)
}

func (t *TradingConfig) applyDefaults(keys keySet) {
	if t == nil {
		return
	}
	applyFieldDefaults(keys,
		stringFieldDefault("trading.mode", &t.Mode, defaultTradingMode),
		fieldDefault{
			key:   "trading.max_position_pct",
			need:  func() bool { return t.MaxPositionPct <= 0 || t.MaxPositionPct > 1 },
			apply: func() { t.MaxPositionPct = defaultTradingMaxPct },
		},
		fieldDefault{
			key:   "trading.default_leverage",
			need:  func() bool { return t.DefaultLeverage <= 0 },
			apply: func() { t.DefaultLeverage = defaultTradingLeverage },
		},
	)
	if t.DefaultPositionUSD < 0 {
		t.DefaultPositionUSD = 0
	}
}

func (f *FreqtradeConfig) applyDefaults(keys keySet) {
	if f == nil {
		return
	}
	applyFieldDefaults(keys,
		boolFieldDefault("freqtrade.enabled", &f.Enabled, true),
		stringFieldDefault("freqtrade.api_url", &f.APIURL, defaultFreqtradeAPI),
		stringFieldDefault("freqtrade.webhook_url", &f.WebhookURL, defaultFreqtradeWebhook),
		stringFieldDefault("freqtrade.risk_store_path", &f.RiskStorePath, defaultFreqtradeRiskDB),
		fieldDefault{
			key:   "freqtrade.default_stake_usd",
			need:  func() bool { return f.DefaultStakeUSD <= 0 },
			apply: func() { f.DefaultStakeUSD = defaultFreqtradeStake },
		},
		fieldDefault{
			key:   "freqtrade.default_leverage",
			need:  func() bool { return f.DefaultLeverage <= 0 },
			apply: func() { f.DefaultLeverage = defaultFreqtradeLev },
		},
		fieldDefault{
			key:   "freqtrade.timeout_seconds",
			need:  func() bool { return f.TimeoutSeconds <= 0 },
			apply: func() { f.TimeoutSeconds = defaultFreqtradeTimeout },
		},
	)
	if f.DefaultStakeUSD < 0 {
		f.DefaultStakeUSD = 0
	}
	if f.MinStopDistancePct < 0 {
		f.MinStopDistancePct = 0
	}
	if f.EntrySlipPct < 0 {
		f.EntrySlipPct = 0
	}
}

func (a *AIConfig) applyDefaults(keys keySet) {
	if a == nil {
		return
	}
	if a.ProviderPresets == nil {
		a.ProviderPresets = make(map[string]ModelPreset)
	}
	applyFieldDefaults(keys,
		stringFieldDefault("ai.aggregation", &a.Aggregation, defaultAIAggregation),
		stringFieldDefault("ai.decision_log_path", &a.DecisionLogPath, defaultAIDecisionLog),
		stringFieldDefault("ai.profiles_path", &a.ProfilesPath, defaultProfilesPath),
		stringFieldDefault("ai.exit_strategies_path", &a.ExitPlanPath, defaultExitPlanPath),
		fieldDefault{
			key:   "ai.decision_offset_seconds",
			need:  func() bool { return a.DecisionOffsetSeconds == 0 },
			apply: func() { a.DecisionOffsetSeconds = defaultAIDecisionOffset },
		},
		boolFieldDefault("ai.log_each_model", &a.LogEachModel, true),
	)
	a.ProviderPreference = normalizePreferenceList(a.ProviderPreference)
	if strings.TrimSpace(a.ActiveHorizon) == "" {
		a.ActiveHorizon = "profiles"
	}
	a.MultiAgent.applyDefaults(keys)
}

func (m *MultiAgentConfig) applyDefaults(keys keySet) {
	if m == nil {
		return
	}
	applyFieldDefaults(keys,
		boolFieldDefault("ai.multi_agent.enabled", &m.Enabled, true),
		stringFieldDefault("ai.multi_agent.indicator_template", &m.IndicatorTemplate, "agent_indicator"),
		stringFieldDefault("ai.multi_agent.pattern_template", &m.PatternTemplate, "agent_pattern"),
		stringFieldDefault("ai.multi_agent.trend_template", &m.TrendTemplate, "agent_trend"),
	)
	if strings.TrimSpace(m.IndicatorProvider) == "" {
		m.IndicatorProvider = "deepseek"
	}
	if strings.TrimSpace(m.PatternProvider) == "" {
		m.PatternProvider = "qwen"
	}
	if strings.TrimSpace(m.TrendProvider) == "" {
		m.TrendProvider = "vanchin"
	}
}

func (m *MarketConfig) applyDefaults(keys keySet) {
	if m == nil {
		return
	}
	if len(m.Sources) == 0 {
		m.Sources = []MarketSource{{
			Name:        defaultMarketName,
			Enabled:     true,
			RESTBaseURL: defaultMarketREST,
		}}
	}
	for i := range m.Sources {
		src := &m.Sources[i]
		src.Proxy.normalize()
		if strings.TrimSpace(src.Name) == "" {
			if i == 0 {
				src.Name = defaultMarketName
			} else {
				src.Name = fmt.Sprintf("market_%d", i)
			}
		}
		if src.RESTBaseURL == "" {
			src.RESTBaseURL = defaultMarketREST
		}
	}
	if strings.TrimSpace(m.ActiveSource) == "" {
		m.ActiveSource = firstEnabledMarket(m.Sources)
	}
}

func applyFieldDefaults(keys keySet, defs ...fieldDefault) {
	for _, def := range defs {
		if def.apply == nil {
			continue
		}
		if def.key != "" && keys.isSet(def.key) {
			continue
		}
		if def.need != nil && !def.need() {
			continue
		}
		def.apply()
	}
}

func stringFieldDefault(key string, target *string, def string) fieldDefault {
	return fieldDefault{
		key: key,
		need: func() bool {
			return target != nil && strings.TrimSpace(*target) == ""
		},
		apply: func() {
			if target != nil {
				*target = def
			}
		},
	}
}

func boolFieldDefault(key string, target *bool, def bool) fieldDefault {
	return fieldDefault{
		key:  key,
		need: func() bool { return target != nil },
		apply: func() {
			if target != nil {
				*target = def
			}
		},
	}
}

func firstEnabledMarket(sources []MarketSource) string {
	for _, src := range sources {
		name := strings.TrimSpace(src.Name)
		if src.Enabled && name != "" {
			return name
		}
	}
	if len(sources) > 0 {
		if name := strings.TrimSpace(sources[0].Name); name != "" {
			return name
		}
	}
	return defaultMarketName
}

func normalizePreferenceList(pref []string) []string {
	if len(pref) == 0 {
		return nil
	}
	out := make([]string, 0, len(pref))
	seen := make(map[string]bool, len(pref))
	for _, id := range pref {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
