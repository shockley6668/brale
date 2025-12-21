package config

import "strings"

type Config struct {
	App       AppConfig       `toml:"app"`
	Kline     KlineConfig     `toml:"kline"`
	Market    MarketConfig    `toml:"market"`
	AI        AIConfig        `toml:"ai"`
	Store     StoreConfig     `toml:"store"`
	MCP       MCPConfig       `toml:"mcp"`
	Prompt    PromptConfig    `toml:"prompt"`
	Notify    NotifyConfig    `toml:"notify"`
	Freqtrade FreqtradeConfig `toml:"freqtrade"`
	Advanced  AdvancedConfig  `toml:"advanced"`
	Trading   TradingConfig   `toml:"trading"`
}

type AppConfig struct {
	Env      string `toml:"env"`
	LogLevel string `toml:"log_level"`
	HTTPAddr string `toml:"http_addr"`
	LogPath  string `toml:"log_path"`
	LLMLog   string `toml:"llm_log_path"`
	LLMDump  bool   `toml:"llm_dump_payload"`
}

type KlineConfig struct {
	MaxCached int `toml:"max_cached"`
}

type StoreConfig struct {
	LiveDBPath string `toml:"live_db_path"`
}

type MCPConfig struct {
	TimeoutSeconds int `toml:"timeout_seconds"`
}

type PromptConfig struct {
	Dir            string `toml:"dir"`
	SystemTemplate string `toml:"system_template"`
}

type NotifyConfig struct {
	Telegram TelegramConfig `toml:"telegram"`
}

type TelegramConfig struct {
	Enabled  bool   `toml:"enabled"`
	BotToken string `toml:"bot_token"`
	ChatID   string `toml:"chat_id"`
}

type AdvancedConfig struct {
	LiquidityFilterUSDM        int     `toml:"liquidity_filter_usd_m"`
	MinRiskReward              float64 `toml:"min_risk_reward"`
	OpenCooldownSeconds        int     `toml:"open_cooldown_seconds"`
	MaxOpensPerCycle           int     `toml:"max_opens_per_cycle"`
	PlanRefreshIntervalSeconds int     `toml:"plan_refresh_interval_seconds"`
}

type TradingConfig struct {
	Mode               string  `toml:"mode"`
	MaxPositionPct     float64 `toml:"max_position_pct"`
	DefaultPositionUSD float64 `toml:"default_position_usd"`
	DefaultLeverage    int     `toml:"default_leverage"`
}

func (t TradingConfig) PositionSizeUSD() float64 {
	if t.DefaultPositionUSD > 0 {
		return t.DefaultPositionUSD
	}
	return 0
}

type FreqtradeConfig struct {
	Enabled            bool    `toml:"enabled"`
	APIURL             string  `toml:"api_url"`
	Username           string  `toml:"username"`
	Password           string  `toml:"password"`
	APIToken           string  `toml:"api_token"`
	DefaultStakeUSD    float64 `toml:"default_stake_usd"`
	DefaultLeverage    int     `toml:"default_leverage"`
	TimeoutSeconds     int     `toml:"timeout_seconds"`
	InsecureSkipVerify bool    `toml:"insecure_skip_verify"`
	WebhookURL         string  `toml:"webhook_url"`
	RiskStorePath      string  `toml:"risk_store_path"`
	MinStopDistancePct float64 `toml:"min_stop_distance_pct"`
	EntrySlipPct       float64 `toml:"entry_slip_pct"`
	EntryTag           string  `toml:"entry_tag"`
	StakeCurrency      string  `toml:"stake_currency"`
}

type AIConfig struct {
	Aggregation           string                 `toml:"aggregation"`
	LogEachModel          bool                   `toml:"log_each_model"`
	Weights               map[string]float64     `toml:"weights"`
	ProviderPreference    []string               `toml:"provider_preference"`
	DecisionOffsetSeconds int                    `toml:"decision_offset_seconds"`
	DecisionLogPath       string                 `toml:"decision_log_path"`
	ActiveHorizon         string                 `toml:"active_horizon"`
	ProviderPresets       map[string]ModelPreset `toml:"provider_presets"`
	Models                []AIModelConfig        `toml:"models"`
	MultiAgent            MultiAgentConfig       `toml:"multi_agent"`
	ProfilesPath          string                 `toml:"profiles_path"`
	ExitPlanPath          string                 `toml:"exit_strategies_path"`
}

type ModelPreset struct {
	APIURL         string            `toml:"api_url"`
	APIKey         string            `toml:"api_key"`
	Headers        map[string]string `toml:"headers"`
	SupportsVision bool              `toml:"supports_vision"`
	ExpectJSON     bool              `toml:"expect_json"`
}

type AIModelConfig struct {
	ID            string            `toml:"id"`
	Provider      string            `toml:"provider"`
	Preset        string            `toml:"preset"`
	Enabled       bool              `toml:"enabled"`
	FinalDisabled bool              `toml:"final_disabled"`
	APIURL        string            `toml:"api_url"`
	APIKey        string            `toml:"api_key"`
	Model         string            `toml:"model"`
	Headers       map[string]string `toml:"headers"`

	SupportsVision *bool `toml:"supports_vision"`
	ExpectJSON     *bool `toml:"expect_json"`
}

type ResolvedModelConfig struct {
	ID             string
	Provider       string
	Enabled        bool
	FinalDisabled  bool
	APIURL         string
	APIKey         string
	Model          string
	Headers        map[string]string
	SupportsVision bool
	ExpectJSON     bool
}

type MultiAgentConfig struct {
	Enabled           bool   `toml:"enabled"`
	IndicatorProvider string `toml:"indicator_provider"`
	PatternProvider   string `toml:"pattern_provider"`
	TrendProvider     string `toml:"trend_provider"`
	IndicatorTemplate string `toml:"indicator_template"`
	PatternTemplate   string `toml:"pattern_template"`
	TrendTemplate     string `toml:"trend_template"`
	MaxBlocks         int    `toml:"max_blocks"`
}

type MarketConfig struct {
	ActiveSource string         `toml:"active_source"`
	Sources      []MarketSource `toml:"sources"`
}

type MarketSource struct {
	Name        string      `toml:"name"`
	Enabled     bool        `toml:"enabled"`
	RESTBaseURL string      `toml:"rest_base_url"`
	Proxy       ProxyConfig `toml:"proxy"`
}

type ProxyConfig struct {
	Enabled bool   `toml:"enabled"`
	RESTURL string `toml:"rest_url"`
	WSURL   string `toml:"ws_url"`
}

func (p *ProxyConfig) normalize() {
	if p == nil {
		return
	}
	p.RESTURL = strings.TrimSpace(p.RESTURL)
	p.WSURL = strings.TrimSpace(p.WSURL)
}

func (m MarketConfig) ResolveActiveSource() MarketSource {
	if len(m.Sources) == 0 {
		return MarketSource{
			Name:        "binance",
			Enabled:     true,
			RESTBaseURL: "https://fapi.binance.com",
		}
	}
	active := strings.ToLower(strings.TrimSpace(m.ActiveSource))
	var fallback MarketSource
	for _, src := range m.Sources {
		if fallback.Name == "" {
			fallback = src
		}
		if !src.Enabled {
			continue
		}
		if active == "" || strings.ToLower(src.Name) == active {
			return src
		}
	}
	return fallback
}

type keySet map[string]struct{}

func (k keySet) mark(path string) {
	path = strings.ToLower(strings.TrimSpace(path))
	if path == "" {
		return
	}
	k[path] = struct{}{}
}

func (k keySet) isSet(path string) bool {
	if len(k) == 0 {
		return false
	}
	path = strings.ToLower(strings.TrimSpace(path))
	if path == "" {
		return false
	}
	_, ok := k[path]
	return ok
}

type fieldDefault struct {
	key   string
	need  func() bool
	apply func()
}
