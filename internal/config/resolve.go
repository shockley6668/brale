package config

import (
	"fmt"
	"strings"
)

func (a AIConfig) ResolveModelConfigs() ([]ResolvedModelConfig, error) {
	out := make([]ResolvedModelConfig, 0, len(a.Models))
	presets := a.ProviderPresets
	for _, raw := range a.Models {
		presetName := strings.TrimSpace(raw.Preset)
		var preset ModelPreset
		if presetName != "" {
			var ok bool
			if presets != nil {
				preset, ok = presets[presetName]
			}
			if !ok {
				return nil, fmt.Errorf("preset ai.provider_presets.%s not found", presetName)
			}
		}
		apiURL := strings.TrimSpace(raw.APIURL)
		if apiURL == "" {
			apiURL = strings.TrimSpace(preset.APIURL)
		}
		apiKey := strings.TrimSpace(raw.APIKey)
		if apiKey == "" {
			apiKey = strings.TrimSpace(preset.APIKey)
		}
		headers := make(map[string]string, len(preset.Headers)+len(raw.Headers))
		for k, v := range preset.Headers {
			headers[k] = v
		}
		for k, v := range raw.Headers {
			headers[k] = v
		}
		supportsVision := preset.SupportsVision
		if raw.SupportsVision != nil {
			supportsVision = *raw.SupportsVision
		}
		expectJSON := preset.ExpectJSON
		if raw.ExpectJSON != nil {
			expectJSON = *raw.ExpectJSON
		}
		out = append(out, ResolvedModelConfig{
			ID:             strings.TrimSpace(raw.ID),
			Provider:       strings.TrimSpace(raw.Provider),
			Enabled:        raw.Enabled,
			FinalDisabled:  raw.FinalDisabled,
			APIURL:         apiURL,
			APIKey:         apiKey,
			Model:          strings.TrimSpace(raw.Model),
			Headers:        headers,
			SupportsVision: supportsVision,
			ExpectJSON:     expectJSON,
		})
	}
	return out, nil
}

func (a AIConfig) MustResolveModelConfigs() []ResolvedModelConfig {
	out, err := a.ResolveModelConfigs()
	if err != nil {
		panic(fmt.Sprintf("MustResolveModelConfigs called on invalid config: %v", err))
	}
	return out
}
