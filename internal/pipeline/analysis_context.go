package pipeline

import (
	"strings"
	"sync"
	"time"

	"brale/internal/market"
	"brale/internal/types"
)

// AnalysisContext 表示某个 symbol 在一次 Pipeline 执行过程中的上下文。
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

// NewContext 初始化上下文。
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

// SetMetadata 写入任意键值。
func (ac *AnalysisContext) SetMetadata(key string, value any) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	ac.metadata[key] = value
}

// Metadata 获取全部信息的副本。
func (ac *AnalysisContext) Metadata() map[string]any {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	out := make(map[string]any, len(ac.metadata))
	for k, v := range ac.metadata {
		out[k] = v
	}
	return out
}

// SetCandles 保存一个周期的 K 线。
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

// Candles 读取一个周期的 K 线副本。
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

// Intervals 返回已有的周期列表。
func (ac *AnalysisContext) Intervals() []string {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	out := make([]string, 0, len(ac.intervals))
	for k := range ac.intervals {
		out = append(out, k)
	}
	return out
}

// AddFeature 记录一个新的特征描述。
func (ac *AnalysisContext) AddFeature(f Feature) {
	if f.Metadata == nil {
		f.Metadata = make(map[string]any)
	}
	ac.mu.Lock()
	defer ac.mu.Unlock()
	ac.features = append(ac.features, f)
}

// Features 返回特征的副本。
func (ac *AnalysisContext) Features() []Feature {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	out := make([]Feature, len(ac.features))
	copy(out, ac.features)
	return out
}

// AppendPromptPart 附加结构化 prompt 片段。
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

// PromptParts 返回所有 prompt 片段。
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

// AddWarning 记录警告。
func (ac *AnalysisContext) AddWarning(msg string) {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return
	}
	ac.mu.Lock()
	defer ac.mu.Unlock()
	ac.warnings = append(ac.warnings, msg)
}

// Warnings 获取告警列表。
func (ac *AnalysisContext) Warnings() []string {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	out := make([]string, len(ac.warnings))
	copy(out, ac.warnings)
	return out
}

// Feature 兼容旧引用，实际定义位于 internal/types。
type Feature = types.Feature
