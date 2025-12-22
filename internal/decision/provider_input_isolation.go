package decision

import (
	"strings"
)

type providerRole string

const (
	roleStructurePattern providerRole = "structure_pattern"
	roleMechanics        providerRole = "mechanics"
	roleIndicator        providerRole = "indicator"
)

func filterAgentInsightsByStage(ins []AgentInsight, allowed map[string]bool) []AgentInsight {
	if len(ins) == 0 {
		return nil
	}
	if allowed == nil {
		return CloneSlice(ins)
	}
	out := make([]AgentInsight, 0, len(ins))
	for _, item := range ins {
		if item.Stage == "" {
			continue
		}
		if allowed[item.Stage] {
			out = append(out, item)
		}
	}
	return out
}

func allowedAgentStagesForProvider(modelID string, prompts map[string]ProfilePromptSpec, candidates []string, configured map[string]string) map[string]bool {
	role := detectProviderRole(modelID, prompts, candidates, configured)
	switch role {
	case roleStructurePattern:
		return map[string]bool{
			agentStageTrend:   true,
			agentStagePattern: true,
		}
	case roleMechanics:
		return map[string]bool{
			agentStageMechanics: true,
		}
	case roleIndicator:
		return map[string]bool{
			agentStageIndicator: true,
		}
	default:
		return nil
	}
}

func detectProviderRole(modelID string, prompts map[string]ProfilePromptSpec, candidates []string, configured map[string]string) providerRole {
	if role := roleFromConfig(modelID, configured); role != "" {
		return role
	}
	ref := providerSystemPromptRef(modelID, prompts, candidates)
	refLower := strings.ToLower(strings.TrimSpace(ref))
	switch {
	case strings.Contains(refLower, "system-structure"), strings.Contains(refLower, "structure"):
		return roleStructurePattern
	case strings.Contains(refLower, "system-mechanics"), strings.Contains(refLower, "mechanics"):
		return roleMechanics
	case strings.Contains(refLower, "system-indicator"), strings.Contains(refLower, "indicator"):
		return roleIndicator
	}
	idLower := strings.ToLower(strings.TrimSpace(modelID))
	switch {
	case strings.Contains(idLower, "structure"):
		return roleStructurePattern
	case strings.Contains(idLower, "mechanics"):
		return roleMechanics
	case strings.Contains(idLower, "indicator"):
		return roleIndicator
	default:
		return ""
	}
}

func roleFromConfig(modelID string, configured map[string]string) providerRole {
	if len(configured) == 0 {
		return ""
	}
	modelID = strings.TrimSpace(modelID)
	role := strings.ToLower(strings.TrimSpace(configured[modelID]))
	switch role {
	case "structure_pattern", "structure", "pattern", "trend_pattern", "trend", "structure-pattern":
		return roleStructurePattern
	case "mechanics", "derivatives", "funding", "oi":
		return roleMechanics
	case "indicator", "indicators", "momentum":
		return roleIndicator
	default:
		return ""
	}
}

func providerSystemPromptRef(modelID string, prompts map[string]ProfilePromptSpec, candidates []string) string {
	if len(prompts) == 0 || len(candidates) != 1 {
		return ""
	}
	target := normalizeSymbol(candidates[0])
	if target == "" {
		return ""
	}
	for sym, spec := range prompts {
		if normalizeSymbol(sym) != target {
			continue
		}
		if len(spec.SystemPromptRefsByModel) == 0 {
			return ""
		}
		return spec.SystemPromptRefsByModel[modelID]
	}
	return ""
}
