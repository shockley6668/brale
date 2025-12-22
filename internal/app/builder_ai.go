package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"brale/internal/analysis/visual"
	brcfg "brale/internal/config"
	"brale/internal/decision"
	"brale/internal/gateway/database"
	"brale/internal/gateway/provider"
	"brale/internal/logger"
	"brale/internal/market"
	"brale/internal/strategy"
)

type engineConfig struct {
	Providers          []provider.ModelProvider
	Aggregator         decision.Aggregator
	PromptMgr          *strategy.Manager
	Store              market.KlineStore
	Intervals          []string
	HorizonName        string
	MultiAgent         brcfg.MultiAgentConfig
	ProviderPreference []string
	ProviderRoles      map[string]string
	StageProviders     map[string]string
	FinalDisabled      map[string]bool
	LogEachModel       bool
	Metrics            *market.MetricsService
	Sentiment          *market.SentimentService
	FearGreed          *market.FearGreedService
	TimeoutSeconds     int
}

type decisionArtifacts struct {
	store *database.DecisionLogStore
}

func buildDecisionArtifacts(ctx context.Context, cfg brcfg.AIConfig, engine *decision.DecisionEngine) (*decisionArtifacts, error) {
	artifacts := &decisionArtifacts{}
	if strings.TrimSpace(cfg.DecisionLogPath) == "" {
		return artifacts, nil
	}
	store, err := database.NewDecisionLogStore(cfg.DecisionLogPath)
	if err != nil {
		return nil, fmt.Errorf("初始化决策日志存储失败: %w", err)
	}
	artifacts.store = store
	if engine != nil {
		if obs := database.NewDecisionLogObserver(store); obs != nil {
			engine.Observer = obs
		}
		engine.AgentHistory = store
		engine.ProviderHistory = store
	}
	logPath := cfg.DecisionLogPath
	if abs, err := filepath.Abs(logPath); err == nil {
		logPath = abs
	}
	logger.Infof("✓ 实盘决策日志写入 %s", logPath)
	return artifacts, nil
}

func buildAggregator(cfg brcfg.AIConfig) decision.Aggregator {
	if strings.EqualFold(cfg.Aggregation, "meta") {
		return decision.MetaAggregator{
			Weights:    cfg.Weights,
			Preference: cfg.ProviderPreference,
		}
	}
	return decision.FirstWinsAggregator{}
}

func buildModelProviders(ctx context.Context, cfg brcfg.AIConfig, timeoutSeconds int) ([]provider.ModelProvider, map[string]bool, bool, error) {
	var (
		modelCfgs   []provider.ModelCfg
		visionReady bool
	)
	finalDisabled := make(map[string]bool)
	for _, m := range cfg.MustResolveModelConfigs() {
		id := strings.TrimSpace(m.ID)
		if id == "" {
			provider := strings.TrimSpace(m.Provider)
			if provider == "" {
				provider = "provider"
			}
			if model := strings.TrimSpace(m.Model); model != "" {
				id = provider + ":" + model
			} else {
				id = provider
			}
			logger.Warnf("未配置 ai.models.id，已为 %q 生成 ID: %s", m.Provider, id)
		}
		modelCfgs = append(modelCfgs, provider.ModelCfg{
			ID:             id,
			Provider:       m.Provider,
			Enabled:        m.Enabled,
			APIURL:         m.APIURL,
			APIKey:         m.APIKey,
			Model:          m.Model,
			Headers:        m.Headers,
			SupportsVision: m.SupportsVision,
			ExpectJSON:     m.ExpectJSON,
		})
		if m.Enabled && m.SupportsVision {
			visionReady = true
		}
		if m.FinalDisabled {
			finalDisabled[id] = true
		}
	}
	if visionReady {
		if err := visual.EnsureHeadlessAvailable(ctx); err != nil {
			return nil, nil, false, fmt.Errorf("初始化可视化渲染失败(请安装 headless Chrome): %w", err)
		}
	} else {
		logger.Infof("所有启用模型均不支持图像，跳过可视化渲染初始化")
	}
	timeout := time.Duration(timeoutSeconds) * time.Second
	providers := provider.BuildProvidersFromConfig(modelCfgs, timeout)
	if len(providers) == 0 {
		logger.Warnf("未启用任何 AI 模型（请检查 ai.models 配置）")
	} else {
		ids := make([]string, 0, len(providers))
		for _, p := range providers {
			if p != nil && p.Enabled() {
				ids = append(ids, p.ID())
			}
		}
		logger.Infof("✓ 已启用 %d 个 AI 模型: %v", len(ids), ids)
	}
	return providers, finalDisabled, visionReady, nil
}

func buildDecisionEngine(cfg engineConfig) *decision.DecisionEngine {
	agg := cfg.Aggregator
	if agg == nil {
		agg = decision.FirstWinsAggregator{}
	}
	var finalDisabled map[string]bool
	if len(cfg.FinalDisabled) > 0 {
		finalDisabled = make(map[string]bool, len(cfg.FinalDisabled))
		for k, v := range cfg.FinalDisabled {
			if v {
				finalDisabled[k] = true
			}
		}
	}
	engine := &decision.DecisionEngine{
		Providers:          cfg.Providers,
		Agg:                agg,
		PromptMgr:          cfg.PromptMgr,
		KStore:             cfg.Store,
		Intervals:          append([]string(nil), cfg.Intervals...),
		HorizonName:        cfg.HorizonName,
		MultiAgent:         cfg.MultiAgent,
		ProviderPreference: append([]string(nil), cfg.ProviderPreference...),
		ProviderRoles:      cfg.ProviderRoles,
		StageProviders:     cfg.StageProviders,
		FinalDisabled:      finalDisabled,
		Parallel:           true,
		LogEachModel:       cfg.LogEachModel,
		TimeoutSeconds:     cfg.TimeoutSeconds,
	}
	engine.PromptBuilder = decision.NewDefaultPromptBuilder(cfg.PromptMgr, cfg.Store, cfg.Metrics, cfg.Sentiment, cfg.FearGreed, cfg.Intervals, cfg.LogEachModel)
	return engine
}

func resolvePersonas(cfg brcfg.AIConfig, providers []provider.ModelProvider) (map[string]string, map[string]string, error) {
	if len(cfg.Personas) == 0 {
		return nil, nil, fmt.Errorf("ai.personas is required")
	}
	providerSet := make(map[string]struct{}, len(providers))
	for _, p := range providers {
		if p != nil && p.Enabled() {
			providerSet[p.ID()] = struct{}{}
		}
	}
	providerRoles := make(map[string]string, len(cfg.Personas))
	stageProviders := make(map[string]string, 4)
	for name, persona := range cfg.Personas {
		modelID := strings.TrimSpace(persona.Model)
		role := brcfg.NormalizePersonaRole(persona.Role)
		if modelID == "" || role == "" {
			return nil, nil, fmt.Errorf("invalid persona %s: model=%q role=%q", name, persona.Model, persona.Role)
		}
		if _, ok := providerSet[modelID]; !ok {
			return nil, nil, fmt.Errorf("persona %s references disabled or missing model %s", name, modelID)
		}
		providerRoles[modelID] = role
		for _, st := range persona.Stages {
			stage := brcfg.NormalizePersonaStage(st)
			if stage == "" {
				return nil, nil, fmt.Errorf("persona %s has invalid stage %q", name, st)
			}
			if prev, exists := stageProviders[stage]; exists && prev != modelID {
				return nil, nil, fmt.Errorf("stage %s bound to multiple models (%s vs %s)", stage, prev, modelID)
			}
			stageProviders[stage] = modelID
		}
	}
	required := []string{"indicator", "pattern", "trend", "mechanics"}
	for _, st := range required {
		if _, ok := stageProviders[st]; !ok && cfg.MultiAgent.Enabled {
			return nil, nil, fmt.Errorf("persona missing stage binding: %s", st)
		}
	}
	return providerRoles, stageProviders, nil
}

func applyDefaultMultiAgentBlocks(cfg *brcfg.Config, symbolCount, intervalCount int) {
	if cfg == nil {
		return
	}
	if cfg.AI.MultiAgent.MaxBlocks > 0 {
		return
	}
	auto := symbolCount * intervalCount
	if auto <= 0 {
		if intervalCount > 0 {
			auto = intervalCount
		} else if symbolCount > 0 {
			auto = symbolCount
		} else {
			auto = 4
		}
	}
	cfg.AI.MultiAgent.MaxBlocks = auto
	logger.Infof("✓ Multi-Agent max_blocks 未配置，自动使用 %d（%d 个币种 × %d 个周期）", auto, symbolCount, intervalCount)
}
