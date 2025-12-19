package pipeline

import (
	"strings"
	"sync"
	"time"

	"brale/internal/market"
	"brale/internal/types"
)

type AnalysisContext struct {
	Symbol     string
	Profile    string
	ContextTag string
	TraceID    string
	StartedAt  time.Time

	mu        sync.RWMutex
	intervals map[string][]market.Candle
	features  []Feature
	prompts   map[string][]string
	warnings  []string
	metadata  map[string]any
}

func NewContext(symbol string) *AnalysisContext {
	return &AnalysisContext{
		Symbol:     strings.ToUpper(strings.TrimSpace(symbol)),
		intervals:  make(map[string][]market.Candle),
		prompts:    make(map[string][]string),
		metadata:   make(map[string]any),
		StartedAt:  time.Now(),
		ContextTag: "default",
	}
}

func (ac *AnalysisContext) SetMetadata(key string, value any) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	ac.metadata[key] = value
}

func (ac *AnalysisContext) Metadata() map[string]any {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	out := make(map[string]any, len(ac.metadata))
	for k, v := range ac.metadata {
		out[k] = v
	}
	return out
}

func (ac *AnalysisContext) SetCandles(interval string, candles []market.Candle) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	iv := strings.TrimSpace(interval)
	if iv == "" {
		return
	}
	normalized := strings.ToLower(iv)
	dst := make([]market.Candle, len(candles))
	copy(dst, candles)
	ac.intervals[normalized] = dst
}

func (ac *AnalysisContext) Candles(interval string) []market.Candle {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	normalized := strings.ToLower(strings.TrimSpace(interval))
	data := ac.intervals[normalized]
	if len(data) == 0 {
		return nil
	}
	out := make([]market.Candle, len(data))
	copy(out, data)
	return out
}

func (ac *AnalysisContext) Intervals() []string {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	out := make([]string, 0, len(ac.intervals))
	for k := range ac.intervals {
		out = append(out, k)
	}
	return out
}

func (ac *AnalysisContext) AddFeature(f Feature) {
	if f.Metadata == nil {
		f.Metadata = make(map[string]any)
	}
	ac.mu.Lock()
	defer ac.mu.Unlock()
	ac.features = append(ac.features, f)
}

func (ac *AnalysisContext) Features() []Feature {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	out := make([]Feature, len(ac.features))
	copy(out, ac.features)
	return out
}

func (ac *AnalysisContext) AppendPromptPart(section string, lines ...string) {
	if len(lines) == 0 {
		return
	}
	ac.mu.Lock()
	defer ac.mu.Unlock()
	sec := strings.TrimSpace(section)
	if sec == "" {
		sec = "default"
	}
	ac.prompts[sec] = append(ac.prompts[sec], lines...)
}

func (ac *AnalysisContext) PromptParts() map[string][]string {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	out := make(map[string][]string, len(ac.prompts))
	for k, v := range ac.prompts {
		cp := make([]string, len(v))
		copy(cp, v)
		out[k] = cp
	}
	return out
}

func (ac *AnalysisContext) AddWarning(msg string) {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return
	}
	ac.mu.Lock()
	defer ac.mu.Unlock()
	ac.warnings = append(ac.warnings, msg)
}

func (ac *AnalysisContext) Warnings() []string {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	out := make([]string, len(ac.warnings))
	copy(out, ac.warnings)
	return out
}

type Feature = types.Feature
