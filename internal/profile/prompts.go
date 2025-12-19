package profile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type TemplateSource interface {
	Get(name string) (string, bool)
}

type PromptLoader interface {
	Load(ref string) (string, error)
}

type FilePromptLoader struct {
	source TemplateSource
	bases  []string
}

func NewPromptLoader(source TemplateSource, bases ...string) *FilePromptLoader {
	normalized := make([]string, 0, len(bases))
	seen := make(map[string]struct{}, len(bases))
	for _, base := range bases {
		base = strings.TrimSpace(base)
		if base == "" {
			continue
		}
		if _, ok := seen[base]; ok {
			continue
		}
		normalized = append(normalized, base)
		seen[base] = struct{}{}
	}
	return &FilePromptLoader{
		source: source,
		bases:  normalized,
	}
}

func (l *FilePromptLoader) Load(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", nil
	}
	if txt, ok := l.loadFromSource(ref); ok {
		return txt, nil
	}
	paths := l.candidatePaths(ref)
	var lastErr error
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err == nil {
			return string(data), nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("prompt %s 未找到", ref)
}

func (l *FilePromptLoader) loadFromSource(ref string) (string, bool) {
	if l == nil || l.source == nil {
		return "", false
	}
	if txt, ok := l.source.Get(ref); ok && strings.TrimSpace(txt) != "" {
		return txt, true
	}
	name := templateKey(ref)
	if name == "" || strings.EqualFold(name, ref) {
		return "", false
	}
	if txt, ok := l.source.Get(name); ok && strings.TrimSpace(txt) != "" {
		return txt, true
	}
	return "", false
}

func (l *FilePromptLoader) candidatePaths(ref string) []string {
	cleaned := filepath.Clean(ref)
	paths := []string{cleaned}
	if filepath.Ext(cleaned) == "" {
		paths = append(paths, cleaned+".txt")
	}
	if filepath.IsAbs(cleaned) {
		return uniqueStrings(paths)
	}
	for _, base := range l.bases {
		paths = append(paths, filepath.Join(base, cleaned))
		if filepath.Ext(cleaned) == "" {
			paths = append(paths, filepath.Join(base, cleaned+".txt"))
		}
	}
	return uniqueStrings(paths)
}

func templateKey(ref string) string {
	base := filepath.Base(ref)
	if ext := filepath.Ext(base); ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	return strings.TrimSpace(base)
}

func uniqueStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
