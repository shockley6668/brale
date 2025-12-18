package config

import (
	"fmt"
	"strings"
)

// validate 对配置进行基础校验。
func validate(c *Config) error {
	if err := c.AI.validate(); err != nil {
		return err
	}
	if err := c.Kline.validate(); err != nil {
		return err
	}
	if err := c.Market.validate(); err != nil {
		return err
	}
	if err := c.Notify.validate(); err != nil {
		return err
	}
	if err := c.Freqtrade.validate(); err != nil {
		return err
	}
	if err := c.Trading.validate(); err != nil {
		return err
	}
	return nil
}

func (a *AIConfig) validate() error {
	if a.DecisionOffsetSeconds < 0 {
		return fmt.Errorf("ai.decision_offset_seconds must be >= 0")
	}
	models, err := a.ResolveModelConfigs()
	if err != nil {
		return err
	}
	if len(models) == 0 {
		return fmt.Errorf("ai.models requires at least one model")
	}
	for _, m := range models {
		if strings.TrimSpace(m.Model) == "" {
			return fmt.Errorf("ai.models contains entry without model (id=%s)", m.ID)
		}
		if strings.TrimSpace(m.APIURL) == "" {
			return fmt.Errorf("ai.models.%s missing api_url (can inherit from preset)", m.ID)
		}
		if strings.TrimSpace(m.Provider) == "" {
			return fmt.Errorf("ai.models.%s missing provider", m.ID)
		}
	}
	if len(a.ProviderPreference) > 0 {
		modelSet := make(map[string]struct{}, len(models))
		for _, m := range models {
			modelSet[m.ID] = struct{}{}
		}
		for _, id := range a.ProviderPreference {
			if _, ok := modelSet[id]; !ok {
				return fmt.Errorf("ai.provider_preference contains unconfigured model id: %s", id)
			}
		}
	}
	if a.MultiAgent.Enabled {
		ma := a.MultiAgent
		if strings.TrimSpace(ma.IndicatorTemplate) == "" {
			return fmt.Errorf("ai.multi_agent.indicator_template cannot be empty")
		}
		if strings.TrimSpace(ma.PatternTemplate) == "" {
			return fmt.Errorf("ai.multi_agent.pattern_template cannot be empty")
		}
		if strings.TrimSpace(ma.TrendTemplate) == "" {
			return fmt.Errorf("ai.multi_agent.trend_template cannot be empty")
		}
		if ma.MaxBlocks < 0 {
			return fmt.Errorf("ai.multi_agent.max_blocks must be >= 0")
		}
	}
	return nil
}

func (k *KlineConfig) validate() error {
	if k.MaxCached < 50 || k.MaxCached > 1000 {
		return fmt.Errorf("kline.max_cached must be in [50,1000]")
	}
	return nil
}

func (m *MarketConfig) validate() error {
	if len(m.Sources) == 0 {
		return fmt.Errorf("market.sources requires at least one source")
	}
	activeName := strings.ToLower(strings.TrimSpace(m.ActiveSource))
	enabled := 0
	activeFound := false
	for _, src := range m.Sources {
		if !src.Enabled {
			continue
		}
		enabled++
		if strings.TrimSpace(src.RESTBaseURL) == "" {
			return fmt.Errorf("market source %s missing rest_base_url", src.Name)
		}
		if src.Proxy.Enabled && src.Proxy.RESTURL == "" && src.Proxy.WSURL == "" {
			return fmt.Errorf("market source %s has proxy enabled but no rest_url or ws_url", src.Name)
		}
		name := strings.ToLower(strings.TrimSpace(src.Name))
		if activeName == "" || name == activeName {
			activeFound = true
		}
	}
	if enabled == 0 {
		return fmt.Errorf("market.sources requires at least one enabled source")
	}
	if !activeFound {
		return fmt.Errorf("enabled market.active_source=%s not found", m.ActiveSource)
	}
	return nil
}

func (n *NotifyConfig) validate() error {
	if n.Telegram.Enabled {
		if n.Telegram.BotToken == "" || n.Telegram.ChatID == "" {
			return fmt.Errorf("telegram notification enabled but missing bot_token or chat_id")
		}
	}
	return nil
}

func (f *FreqtradeConfig) validate() error {
	if !f.Enabled {
		return nil
	}
	if strings.TrimSpace(f.APIURL) == "" {
		return fmt.Errorf("freqtrade.api_url cannot be empty")
	}
	if strings.TrimSpace(f.APIToken) == "" {
		if strings.TrimSpace(f.Username) == "" || strings.TrimSpace(f.Password) == "" {
			return fmt.Errorf("freqtrade requires api_token or username+password")
		}
	}
	if f.DefaultStakeUSD < 0 {
		return fmt.Errorf("freqtrade.default_stake_usd must be >= 0")
	}
	if f.DefaultLeverage < 0 {
		return fmt.Errorf("freqtrade.default_leverage must be >= 0")
	}
	if f.MinStopDistancePct < 0 {
		return fmt.Errorf("freqtrade.min_stop_distance_pct must be >= 0")
	}
	if f.EntrySlipPct < 0 {
		return fmt.Errorf("freqtrade.entry_slip_pct must be >= 0")
	}
	return nil
}

func (t *TradingConfig) validate() error {
	if t.Mode == "" {
		t.Mode = "static"
	}
	if t.Mode != "static" {
		return fmt.Errorf("trading.mode only supports 'static', got %s", t.Mode)
	}
	if t.MaxPositionPct <= 0 || t.MaxPositionPct > 1 {
		return fmt.Errorf("trading.max_position_pct must be in (0, 1]")
	}
	if t.DefaultLeverage <= 0 {
		return fmt.Errorf("trading.default_leverage must be > 0")
	}
	return nil
}

// IsValidInterval 简易校验：以数字开头，以 m/h/d/w 结尾
func IsValidInterval(s string) bool {
	if s == "" {
		return false
	}
	suf := s[len(s)-1]
	if suf != 'm' && suf != 'h' && suf != 'd' && suf != 'w' {
		return false
	}
	for i := 0; i < len(s)-1; i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
