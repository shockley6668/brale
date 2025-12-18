package exitplan

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"brale/internal/logger"

	"github.com/fsnotify/fsnotify"
	"github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// Template 描述单个退出计划模板。
type Template struct {
	ID          string                 `mapstructure:"id" yaml:"id"`
	Description string                 `mapstructure:"description" yaml:"description"`
	Handler     string                 `mapstructure:"handler" yaml:"handler"`
	Version     int                    `mapstructure:"version" yaml:"version"`
	PromptHint  string                 `mapstructure:"prompt_hint" yaml:"prompt_hint"`
	Schema      map[string]interface{} `mapstructure:"schema" yaml:"schema"`

	schemaCompiled *jsonschema.Schema
}

// FileConfig 映射 exit_plans。
type FileConfig struct {
	ExitPlans map[string]Template `mapstructure:"exit_plans" yaml:"exit_plans"`
}

// Snapshot 公开的模板快照。
type Snapshot struct {
	Version   int64
	LoadedAt  time.Time
	Templates map[string]Template
}

// ChangeListener 在 registry 重载时触发。
type ChangeListener func(Snapshot)

// Registry 管理 exit plan 模板。
type Registry struct {
	path string
	v    *viper.Viper

	mu        sync.RWMutex
	snapshot  Snapshot
	listeners []ChangeListener
}

// NewRegistry 读取配置文件并监听更新。
func NewRegistry(path string) (*Registry, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("exit plan registry requires path")
	}
	v := viper.New()
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read exit plan config failed: %w", err)
	}
	r := &Registry{path: path, v: v}
	if err := r.reload(); err != nil {
		return nil, err
	}
	v.OnConfigChange(func(evt fsnotify.Event) {
		if err := r.reload(); err != nil {
			logger.Errorf("exit plan reload failed: %v", err)
			return
		}
		r.notifyListeners()
	})
	v.WatchConfig()
	return r, nil
}

// Snapshot 返回当前模板集。
func (r *Registry) Snapshot() Snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return cloneSnapshot(r.snapshot)
}

// Template 返回指定 ID 的模板。
func (r *Registry) Template(id string) (Template, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tpl, ok := r.snapshot.Templates[strings.TrimSpace(id)]
	return tpl, ok
}

// AllowedSchema 根据 ID 列表渲染提示片段。
func (r *Registry) AllowedSchema(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n## Exit Plan Templates\n")
	seen := make(map[string]bool)
	for idx, raw := range ids {
		id := strings.TrimSpace(raw)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		tpl, ok := r.Template(id)
		if !ok {
			continue
		}
		title := fmt.Sprintf("%d. %s (v%d)", idx+1, tpl.ID, tpl.Version)
		if tpl.Description != "" {
			title += " - " + tpl.Description
		}
		b.WriteString(title + "\n")
		if hint := strings.TrimSpace(tpl.PromptHint); hint != "" {
			b.WriteString("   Hint:\n")
			b.WriteString("   " + indentLines(hint, "   "))
			b.WriteString("\n")
		}
	}
	return b.String()
}

func (r *Registry) reload() error {
	cfg, err := readExitPlanFile(r.path)
	if err != nil {
		return err
	}
	templates := make(map[string]Template)
	for name, tpl := range cfg.ExitPlans {
		norm := normalizeTemplate(name, tpl)
		templates[norm.ID] = norm
	}
	r.mu.Lock()
	r.snapshot = Snapshot{
		Version:   r.snapshot.Version + 1,
		LoadedAt:  time.Now(),
		Templates: templates,
	}
	r.mu.Unlock()
	logger.Infof("Exit plan registry loaded %d templates from %s", len(templates), filepath.Base(r.path))
	return nil
}

func (r *Registry) notifyListeners() {
	r.mu.RLock()
	snap := cloneSnapshot(r.snapshot)
	listeners := append([]ChangeListener(nil), r.listeners...)
	r.mu.RUnlock()
	for _, fn := range listeners {
		if fn == nil {
			continue
		}
		go func(cb ChangeListener) {
			defer safeRecover("exit plan listener")
			cb(snap)
		}(fn)
	}
}

func normalizeTemplate(name string, tpl Template) Template {
	tpl.ID = strings.TrimSpace(tpl.ID)
	if tpl.ID == "" {
		tpl.ID = strings.TrimSpace(name)
	}
	if tpl.Version <= 0 {
		tpl.Version = 1
	}
	tpl.Description = strings.TrimSpace(tpl.Description)
	if len(tpl.Schema) > 0 {
		if compiled, err := compileSchema(tpl.Schema); err != nil {
			logger.Errorf("exit plan schema compile failed id=%s: %v", tpl.ID, err)
		} else {
			tpl.schemaCompiled = compiled
		}
	}
	return tpl
}

func cloneSnapshot(src Snapshot) Snapshot {
	dst := Snapshot{
		Version:   src.Version,
		LoadedAt:  src.LoadedAt,
		Templates: make(map[string]Template, len(src.Templates)),
	}
	for id, tpl := range src.Templates {
		dst.Templates[id] = tpl
	}
	return dst
}

func indentLines(text, prefix string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = prefix + strings.TrimRight(line, " ")
	}
	return strings.Join(lines, "\n")
}

func safeRecover(tag string) {
	if r := recover(); r != nil {
		logger.Errorf("%s panic: %v", tag, r)
	}
}

func compileSchema(data map[string]interface{}) (*jsonschema.Schema, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("schema.json", strings.NewReader(string(raw))); err != nil {
		return nil, err
	}
	return compiler.Compile("schema.json")
}

func readExitPlanFile(path string) (FileConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return FileConfig{}, fmt.Errorf("read exit plan config failed: %w", err)
	}
	var cfg FileConfig
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return FileConfig{}, fmt.Errorf("parse exit plan config failed: %w", err)
	}
	return cfg, nil
}

func (t Template) Validate(params map[string]any) error {
	if t.schemaCompiled == nil {
		return nil
	}
	sanitized := sanitizeParams(params)
	return t.schemaCompiled.Validate(sanitized)
}

// sanitizeParams 递归遍历 params，将字符串形式的数字转为 float64，以兼容 LLM 有时返回 "3000" 而非 3000 的情况。
func sanitizeParams(v any) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, child := range val {
			out[k] = sanitizeParams(child)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, child := range val {
			out[i] = sanitizeParams(child)
		}
		return out
	case string:
		// 尝试转换为数字
		s := strings.TrimSpace(val)
		if s == "" {
			return val
		}
		if num, err := strconv.ParseFloat(s, 64); err == nil {
			return num
		}
		return val
	default:
		return val
	}
}

// Validate 调用模板校验指定 plan。
func (r *Registry) Validate(planID string, params map[string]any) (Template, error) {
	tpl, ok := r.Template(planID)
	if !ok {
		return Template{}, fmt.Errorf("unknown exit_plan: %s", planID)
	}
	if err := tpl.Validate(params); err != nil {
		return Template{}, err
	}
	return tpl, nil
}

// MergeAllowedSchemas 合并 profile 中配置的所有计划 ID。
func MergeAllowedSchemas(profilePlans [][]string) []string {
	uniq := make(map[string]struct{})
	for _, plans := range profilePlans {
		for _, id := range plans {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			uniq[id] = struct{}{}
		}
	}
	if len(uniq) == 0 {
		return nil
	}
	out := make([]string, 0, len(uniq))
	for id := range uniq {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// PlanAllowed checks if a plan ID is in the allowed list (case-insensitive).
func PlanAllowed(target string, allowed []string) bool {
	target = strings.TrimSpace(target)
	if target == "" || len(allowed) == 0 {
		return false
	}
	for _, id := range allowed {
		if strings.EqualFold(strings.TrimSpace(id), target) {
			return true
		}
	}
	return false
}
