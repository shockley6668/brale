package config

import (
	"fmt"
	"strings"
)

// 默认值常量
const (
	defaultAppEnv              = "dev"
	defaultAppLogLevel         = "info"
	defaultAppHTTPAddr         = ":9991"
	defaultAppLogPath          = "/data/logs/brale-live.log"
	defaultAppLLMLogPath       = "/data/logs/brale-llm.log"
	defaultKlineMaxCached      = 300
	defaultMarketName          = "binance"
	defaultMarketREST          = "https://fapi.binance.com"
	defaultAIAggregation       = "meta"
	defaultAIDecisionLog       = "/data/live/decisions.db"
	defaultAIDecisionOffset    = 10
	defaultMCPTimeout          = 300
	defaultFreqtradeAPI        = "http://freqtrade:8080/api/v1"
	defaultFreqtradeStake      = 100
	defaultFreqtradeLev        = 1
	defaultFreqtradeTimeout    = 15
	defaultFreqtradeWebhook    = "http://brale:9991/api/live/freqtrade/webhook"
	defaultFreqtradeRiskDB     = "/data/db/trade_risk.db"
	defaultAdvancedLiquidity   = 15
	defaultAdvancedRR          = 1
	defaultAdvancedCooldown    = 180
	defaultAdvancedMaxOpen     = 3
	defaultAdvancedPlanRefresh = 5
	defaultTradingMode         = "static"
	defaultTradingMaxPct       = 0.01
	defaultTradingLeverage     = 10
	defaultProfilesPath        = "configs/profiles.yaml"
	defaultExitPlanPath        = "configs/exit_strategies.yaml"
)

// applyDefaults 为所有子配置应用默认值。
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

// Helper functions

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
