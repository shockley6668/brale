package provider

import (
	"fmt"
	"strings"
	"time"

	"brale/internal/logger"
)

type ModelCfg struct {
	ID, Provider, APIURL, APIKey, Model string
	Enabled                             bool
	Headers                             map[string]string
	SupportsVision                      bool
	ExpectJSON                          bool
}

func BuildProvidersFromConfig(models []ModelCfg, timeout time.Duration) []ModelProvider {
	out := make([]ModelProvider, 0, len(models))
	for _, m := range models {
		if !m.Enabled {
			continue
		}
		id := strings.TrimSpace(m.ID)
		if id == "" {
			base := strings.TrimSpace(m.Provider)
			if base == "" {
				base = "provider"
			}
			model := strings.TrimSpace(m.Model)
			if model != "" {
				id = fmt.Sprintf("%s:%s", base, model)
			} else {
				id = base
			}
			logger.Warnf("未配置 ai.models.id，已为 %q 生成 ID: %s", m.Provider, id)
		}
		client := &OpenAIChatClient{
			BaseURL:      m.APIURL,
			APIKey:       m.APIKey,
			Model:        m.Model,
			ExtraHeaders: m.Headers,
		}
		if timeout > 0 {
			client.Timeout = timeout
		}
		out = append(out, NewOpenAIModelProvider(id, true, m.SupportsVision, m.ExpectJSON, client))
	}
	return out
}
