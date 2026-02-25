package config

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mitchellh/mapstructure"
	"github.com/spf13/viper"
)

const AIDecisionRunImmediately = true

func Load(path string) (*Config, error) {
	files, err := resolveConfigIncludes(path)
	if err != nil {
		return nil, err
	}
	v := viper.New()
	v.SetConfigType("yaml")
	for _, file := range files {
		if err := mergeConfigFile(v, file); err != nil {
			return nil, fmt.Errorf("reading config file failed (%s): %w", file, err)
		}
	}
	var cfg Config
	if err := v.Unmarshal(&cfg, func(dc *mapstructure.DecoderConfig) {
		dc.TagName = "toml"
		dc.WeaklyTypedInput = true
	}); err != nil {
		return nil, fmt.Errorf("parsing config failed: %w", err)
	}
	setKeys := make(keySet)
	collectSettingsKeys(v.AllSettings(), setKeys)
	cfg.applyDefaults(setKeys)
	if err := validate(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func mergeConfigFile(v *viper.Viper, path string) error {
	tmp := viper.New()
	tmp.SetConfigFile(path)
	if err := tmp.ReadInConfig(); err != nil {
		return err
	}
	return v.MergeConfigMap(tmp.AllSettings())
}

func resolveConfigIncludes(path string) ([]string, error) {
	if path == "" {
		return nil, fmt.Errorf("config path cannot be empty")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	stack := make(map[string]bool)
	files, err := collectConfigFiles(abs, seen, stack)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return []string{abs}, nil
	}
	return files, nil
}

func collectConfigFiles(path string, seen, stack map[string]bool) ([]string, error) {
	path = filepath.Clean(path)
	if stack[path] {
		return nil, fmt.Errorf("include cycle detected: %s", path)
	}
	if seen[path] {
		return nil, nil
	}
	stack[path] = true
	includes, err := parseIncludeList(path)
	if err != nil {
		return nil, fmt.Errorf("parsing include failed (%s): %w", path, err)
	}
	dir := filepath.Dir(path)
	var ordered []string
	for _, inc := range includes {
		inc = strings.TrimSpace(inc)
		if inc == "" {
			continue
		}
		incPath := inc
		if !filepath.IsAbs(inc) {
			incPath = filepath.Join(dir, inc)
		}
		sub, err := collectConfigFiles(incPath, seen, stack)
		if err != nil {
			return nil, err
		}
		if len(sub) > 0 {
			ordered = append(ordered, sub...)
		}
	}
	delete(stack, path)
	seen[path] = true
	ordered = append(ordered, path)
	return ordered, nil
}

func parseIncludeList(path string) ([]string, error) {
	v := viper.New()
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		return nil, err
	}
	raw := v.Get("include")
	if raw == nil {
		return nil, nil
	}
	switch val := raw.(type) {
	case []any:
		out := make([]string, 0, len(val))
		for _, item := range val {
			str, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("include only supports strings")
			}
			str = strings.TrimSpace(str)
			if str != "" {
				out = append(out, str)
			}
		}
		return out, nil
	case []string:
		out := make([]string, 0, len(val))
		for _, item := range val {
			item = strings.TrimSpace(item)
			if item != "" {
				out = append(out, item)
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("include must be a string array")
	}
}

func collectSettingsKeys(settings map[string]any, dest keySet) {
	if dest == nil || len(settings) == 0 {
		return
	}
	flattenConfigKeys("", settings, dest)
}

func flattenConfigKeys(prefix string, node any, dest keySet) {
	switch val := node.(type) {
	case map[string]any:
		for k, v := range val {
			next := strings.ToLower(strings.TrimSpace(k))
			if next == "" {
				continue
			}
			if prefix != "" {
				next = prefix + "." + next
			}
			flattenConfigKeys(next, v, dest)
		}
	case map[interface{}]interface{}:
		for k, v := range val {
			keyStr, ok := k.(string)
			if !ok {
				continue
			}
			next := strings.ToLower(strings.TrimSpace(keyStr))
			if next == "" {
				continue
			}
			if prefix != "" {
				next = prefix + "." + next
			}
			flattenConfigKeys(next, v, dest)
		}
	case []any:
		if prefix != "" {
			dest.mark(prefix)
		}
		for _, item := range val {
			flattenConfigKeys(prefix, item, dest)
		}
	default:
		if prefix != "" {
			dest.mark(prefix)
		}
	}
}
