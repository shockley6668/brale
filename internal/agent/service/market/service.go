package market

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"brale/internal/agent/interfaces"
	"brale/internal/config"
	"brale/internal/decision"
	"brale/internal/market"
	"brale/internal/profile"
	"brale/internal/store"
)

// Service implements interfaces.MarketService.
type Service struct {
	cfg        *config.Config
	ks         market.KlineStore
	profileMgr *profile.Manager

	// Optional monitor to get realtime prices if not using kline store for that
	// In legacy, PriceMonitor was used. We can assume we have access to it or similar capability.
	// For now, let's inject a "PriceSource" interface or just reuse market.KlineStore if it supports it.
	// Legacy Monitor used `Updater` or `KlineStore` to get latest price.
	monitor PriceSource

	// Cache
	indicatorMu   sync.RWMutex
	indicatorSnap map[string]indicatorSnapshot

	hIntervals  []string
	horizonName string
	visionReady bool
}

type PriceSource interface {
	LatestPrice(ctx context.Context, symbol string) float64
}

type ServiceParams struct {
	Config      *config.Config
	KlineStore  market.KlineStore
	ProfileMgr  *profile.Manager
	Monitor     PriceSource
	Intervals   []string
	HorizonName string
	VisionReady bool
}

func NewService(p ServiceParams) *Service {
	return &Service{
		cfg:           p.Config,
		ks:            p.KlineStore,
		profileMgr:    p.ProfileMgr,
		monitor:       p.Monitor,
		hIntervals:    p.Intervals,
		horizonName:   p.HorizonName,
		visionReady:   p.VisionReady,
		indicatorSnap: make(map[string]indicatorSnapshot),
	}
}

// Ensure implementation
var _ interfaces.MarketService = (*Service)(nil)

type indicatorSnapshot struct {
	ATR       float64
	UpdatedAt time.Time
}

func (s *Service) GetAnalysisContexts(ctx context.Context, symbols []string) ([]decision.AnalysisContext, error) {
	exporter, ok := s.ks.(store.SnapshotExporter)
	if !ok || s.profileMgr == nil {
		return nil, nil // Or error? Legacy returned nil
	}

	out := make([]decision.AnalysisContext, 0, len(symbols))
	for _, sym := range symbols {
		symbol := strings.ToUpper(strings.TrimSpace(sym))
		if symbol == "" {
			continue
		}
		rt, ok := s.profileMgr.Resolve(symbol)
		if !ok || rt == nil || rt.AnalysisSlice <= 0 {
			continue
		}

		intervals := rt.Definition.IntervalsLower()
		if len(intervals) == 0 {
			intervals = s.hIntervals
		}
		if len(intervals) == 0 {
			intervals = []string{"1h"} // Fallback
		}

		input := decision.AnalysisBuildInput{
			Context:           ctx,
			Exporter:          exporter,
			Symbols:           []string{symbol},
			Intervals:         intervals,
			Limit:             s.cfg.Kline.MaxCached,
			SliceLength:       rt.AnalysisSlice,
			SliceDrop:         rt.SliceDropTail,
			HorizonName:       s.horizonName,
			IndicatorLookback: rt.IndicatorBars,
			WithImages:        s.visionReady,
			DisableIndicators: !rt.AgentEnabled,
			RequireATR:        profileNeedsATR(rt),
		}
		out = append(out, decision.BuildAnalysisContexts(input)...)
	}
	return out, nil
}

func (s *Service) LatestPrice(ctx context.Context, symbol string) float64 {
	if s.monitor != nil {
		return s.monitor.LatestPrice(ctx, symbol)
	}
	return 0
}

func (s *Service) CaptureIndicators(ctxs []decision.AnalysisContext) {
	if len(ctxs) == 0 {
		return
	}
	now := time.Now()
	s.indicatorMu.Lock()
	defer s.indicatorMu.Unlock()

	for _, c := range ctxs {
		if strings.TrimSpace(c.IndicatorJSON) == "" {
			continue
		}
		var payload struct {
			Data struct {
				ATR *struct {
					Latest float64 `json:"latest"`
				} `json:"atr"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(c.IndicatorJSON), &payload); err != nil {
			continue
		}
		atr := 0.0
		if payload.Data.ATR != nil {
			atr = payload.Data.ATR.Latest
		}
		if atr != 0 {
			sym := strings.ToUpper(strings.TrimSpace(c.Symbol))
			s.indicatorSnap[sym] = indicatorSnapshot{
				ATR:       atr,
				UpdatedAt: now,
			}
		}
	}
}

func (s *Service) GetATR(symbol string) (float64, bool) {
	s.indicatorMu.RLock()
	defer s.indicatorMu.RUnlock()
	snap, ok := s.indicatorSnap[strings.ToUpper(symbol)]
	if !ok {
		return 0, false
	}
	if time.Since(snap.UpdatedAt) > 30*time.Minute {
		return 0, false
	}
	return snap.ATR, true
}

func profileNeedsATR(rt *profile.Runtime) bool {
	if rt == nil {
		return false
	}
	binding := rt.Definition.ExitPlans
	for _, id := range binding.Allowed {
		if containsATRKeyword(id) {
			return true
		}
	}
	for _, combo := range binding.Combos {
		if containsATRKeyword(combo) {
			return true
		}
	}
	return false
}

func containsATRKeyword(val string) bool {
	val = strings.ToLower(strings.TrimSpace(val))
	return strings.Contains(val, "atr")
}
