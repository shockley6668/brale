package loader

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"brale/internal/logger"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

type ProfileDefinition struct {
	Name       string   `mapstructure:"-"`
	ContextTag string   `mapstructure:"context_tag"`
	Targets    []string `mapstructure:"targets"`
	Intervals  []string `mapstructure:"intervals"`

	DecisionIntervalMultiple int                `mapstructure:"decision_interval_multiple"`
	AnalysisSlice            int                `mapstructure:"analysis_slice"`
	SliceDropTail            int                `mapstructure:"slice_drop_tail"`
	Middlewares              []MiddlewareConfig `mapstructure:"middlewares"`
	Prompts                  PromptRefs         `mapstructure:"prompts"`
	ExitPlans                ExitPlanBinding    `mapstructure:"exit_plans"`
	Derivatives              DerivativesConfig  `mapstructure:"derivatives"`
	KlineWindows             KlineWindowConfig  `mapstructure:"kline_windows"`
	Default                  bool               `mapstructure:"default"`

	targetsUpper   []string
	intervalsLower []string
}

func (d ProfileDefinition) ExitPlanCombos() []string {
	return d.ExitPlans.ComboKeys()
}

type PromptRefs struct {
	SystemByModel map[string]string `mapstructure:"system_by_model"`
	User          string            `mapstructure:"user"`
}

const defaultExitPlanID = "plan_combo_main"

type ExitPlanBinding struct {
	Allowed []string `mapstructure:"-"`
	Combos  []string `mapstructure:"combos"`

	allowedNormalized []string
	combosNormalized  []string
}

func (b *ExitPlanBinding) normalize() {

	b.allowedNormalized = []string{defaultExitPlanID}
	b.combosNormalized = normalizeComboKeys(b.Combos)
	b.Allowed = append([]string(nil), b.allowedNormalized...)
	b.Combos = append([]string(nil), b.combosNormalized...)
}

func (b ExitPlanBinding) ComboKeys() []string {
	if len(b.combosNormalized) == 0 {
		return nil
	}
	out := make([]string, len(b.combosNormalized))
	copy(out, b.combosNormalized)
	return out
}

type DerivativesConfig struct {
	Enabled          bool `mapstructure:"enabled"`
	IncludeOI        bool `mapstructure:"include_oi"`
	IncludeFunding   bool `mapstructure:"include_funding"`
	IncludeFearGreed bool `mapstructure:"include_fear_greed"`
}

func (d *DerivativesConfig) normalize() {
	if d == nil {
		return
	}
	if !d.Enabled {
		d.IncludeOI = false
		d.IncludeFunding = false
		d.IncludeFearGreed = false
		return
	}

	if !d.IncludeOI && !d.IncludeFunding && !d.IncludeFearGreed {
		d.IncludeOI = true
		d.IncludeFunding = true
	}
}

type KlineWindowConfig struct {
	Enabled *bool `mapstructure:"enabled"`
}

func (k *KlineWindowConfig) normalize() {
	if k == nil {
		return
	}
	if k.Enabled == nil {
		enabled := true
		k.Enabled = &enabled
	}
}

func (k KlineWindowConfig) IsEnabled() bool {
	if k.Enabled == nil {
		return true
	}
	return *k.Enabled
}

type MiddlewareConfig struct {
	Name           string                            `mapstructure:"name"`
	Stage          int                               `mapstructure:"stage"`
	Critical       bool                              `mapstructure:"critical"`
	TimeoutSeconds int                               `mapstructure:"timeout_seconds"`
	Params         map[string]interface{}            `mapstructure:"params"`
	Configs        map[string]map[string]interface{} `mapstructure:"configs"`
}

type FileConfig struct {
	Profiles map[string]ProfileDefinition `mapstructure:"profiles"`
}

type ProfileSnapshot struct {
	Version  int64
	LoadedAt time.Time
	Profiles map[string]ProfileDefinition
}

type ChangeListener func(ProfileSnapshot)

type ProfileLoader struct {
	path string
	v    *viper.Viper

	mu        sync.RWMutex
	snapshot  ProfileSnapshot
	listeners []ChangeListener
}

func NewProfileLoader(path string) (*ProfileLoader, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("profile loader requires path")
	}
	v := viper.New()
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read profile config failed: %w", err)
	}
	loader := &ProfileLoader{path: path, v: v}
	if err := loader.reload(); err != nil {
		return nil, err
	}
	v.OnConfigChange(func(evt fsnotify.Event) {
		if err := loader.reload(); err != nil {
			logger.Errorf("profile reload failed (%s): %v", evt.Name, err)
			return
		}
		loader.notify()
	})
	v.WatchConfig()
	return loader, nil
}

func (l *ProfileLoader) Snapshot() ProfileSnapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return cloneSnapshot(l.snapshot)
}

func (l *ProfileLoader) Subscribe(fn ChangeListener) {
	if fn == nil {
		return
	}
	l.mu.Lock()
	l.listeners = append(l.listeners, fn)
	snap := cloneSnapshot(l.snapshot)
	l.mu.Unlock()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Errorf("profile listener panic: %v", r)
			}
		}()
		fn(snap)
	}()
}

func (l *ProfileLoader) notify() {
	l.mu.RLock()
	snap := cloneSnapshot(l.snapshot)
	listeners := append([]ChangeListener(nil), l.listeners...)
	l.mu.RUnlock()
	for _, fn := range listeners {
		if fn == nil {
			continue
		}
		go func(cb ChangeListener) {
			defer func() {
				if r := recover(); r != nil {
					logger.Errorf("profile listener panic: %v", r)
				}
			}()
			cb(snap)
		}(fn)
	}
}

func (l *ProfileLoader) reload() error {
	var fileCfg FileConfig
	if err := l.v.Unmarshal(&fileCfg); err != nil {
		return fmt.Errorf("parse profile config failed: %w", err)
	}
	normalized := make(map[string]ProfileDefinition)
	for name, def := range fileCfg.Profiles {
		norm := normalizeProfileDefinition(name, def)
		normalized[name] = norm
	}
	l.mu.Lock()
	l.snapshot = ProfileSnapshot{
		Version:  l.snapshot.Version + 1,
		LoadedAt: time.Now(),
		Profiles: normalized,
	}
	l.mu.Unlock()
	logger.Infof("Profile loader reloaded %d profiles from %s", len(normalized), filepath.Base(l.path))
	return nil
}

func normalizeProfileDefinition(name string, def ProfileDefinition) ProfileDefinition {
	def.Name = name
	def.ContextTag = strings.TrimSpace(def.ContextTag)
	if def.ContextTag == "" {
		def.ContextTag = name
	}
	if def.DecisionIntervalMultiple <= 0 {
		def.DecisionIntervalMultiple = 1
	}
	def.targetsUpper = normalizeSymbols(def.Targets)
	def.intervalsLower = normalizeIntervals(def.Intervals)
	if len(def.Middlewares) == 0 {
		def.Middlewares = []MiddlewareConfig{{
			Name:     "kline_fetcher",
			Critical: true,
		}}
	}
	def.Middlewares = expandMiddlewareConfigs(def.Middlewares)
	def.ExitPlans.normalize()
	def.Derivatives.normalize()
	def.KlineWindows.normalize()
	return def
}

func normalizeSymbols(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, sym := range in {
		s := strings.ToUpper(strings.TrimSpace(sym))
		if s == "" {
			continue
		}
		out = append(out, s)
	}
	return out
}

func normalizeComboKeys(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	out := make([]string, 0, len(in))
	for _, key := range in {
		norm := strings.ToLower(strings.TrimSpace(key))
		if norm == "" {
			continue
		}
		if _, ok := seen[norm]; ok {
			continue
		}
		seen[norm] = struct{}{}
		out = append(out, norm)
	}
	return out
}

func normalizeIntervals(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, iv := range in {
		s := strings.TrimSpace(iv)
		if s == "" {
			continue
		}
		out = append(out, strings.ToLower(s))
	}
	return out
}

func expandMiddlewareConfigs(list []MiddlewareConfig) []MiddlewareConfig {
	if len(list) == 0 {
		return list
	}
	expanded := make([]MiddlewareConfig, 0, len(list))
	for _, cfg := range list {
		children := expandSingleMiddlewareConfig(cfg)
		if len(children) == 0 {
			continue
		}
		expanded = append(expanded, children...)
	}
	return expanded
}

func expandSingleMiddlewareConfig(cfg MiddlewareConfig) []MiddlewareConfig {
	if len(cfg.Configs) == 0 {
		cfg.Params = cloneParams(cfg.Params)
		return []MiddlewareConfig{cfg}
	}
	keys := make([]string, 0, len(cfg.Configs))
	for k := range cfg.Configs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		cfg.Configs = nil
		cfg.Params = cloneParams(cfg.Params)
		return []MiddlewareConfig{cfg}
	}

	out := make([]MiddlewareConfig, 0, len(keys))
	for _, k := range keys {
		spec := cfg.Configs[k]

		params := cloneParams(cfg.Params)
		if params == nil {
			params = make(map[string]interface{})
		}

		for pk, pv := range spec {
			params[pk] = pv
		}

		if _, ok := params["interval"]; !ok {
			if trimmed := strings.TrimSpace(k); trimmed != "" {
				params["interval"] = trimmed
			}
		}
		child := cfg
		child.Params = params
		child.Configs = nil
		out = append(out, child)
	}
	return out
}

func cloneParams(src map[string]interface{}) map[string]interface{} {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func (p ProfileDefinition) TargetsUpper() []string {
	out := make([]string, len(p.targetsUpper))
	copy(out, p.targetsUpper)
	return out
}

func (p ProfileDefinition) IntervalsLower() []string {
	out := make([]string, len(p.intervalsLower))
	copy(out, p.intervalsLower)
	return out
}

func (p ProfileDefinition) AgentEnabled() bool {
	for _, mw := range p.Middlewares {
		if isAgentMiddleware(mw.Name) {
			return true
		}
	}
	return false
}

func (p ProfileDefinition) DerivativesEnabled() bool {
	return p.Derivatives.Enabled && (p.Derivatives.IncludeOI || p.Derivatives.IncludeFunding || p.Derivatives.IncludeFearGreed)
}

func (p ProfileDefinition) DerivativesMetricsEnabled() bool {
	return p.Derivatives.Enabled && (p.Derivatives.IncludeOI || p.Derivatives.IncludeFunding)
}

func (p ProfileDefinition) KlineWindowsEnabled() bool {
	return p.KlineWindows.IsEnabled()
}

func cloneSnapshot(src ProfileSnapshot) ProfileSnapshot {
	dst := ProfileSnapshot{
		Version:  src.Version,
		LoadedAt: src.LoadedAt,
		Profiles: make(map[string]ProfileDefinition, len(src.Profiles)),
	}
	for name, def := range src.Profiles {
		dst.Profiles[name] = def
	}
	return dst
}

func isAgentMiddleware(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "ema_trend", "rsi_extreme", "macd_trend":
		return true
	default:
		return false
	}
}
