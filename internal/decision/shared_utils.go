package decision

import (
	"encoding/json"
	"sort"
	"strings"

	"brale/internal/gateway/provider"
	"brale/internal/logger"
	"brale/internal/market"
	"brale/internal/pkg/text"
)

// Shared utilities for decision package to avoid duplication between legacy and standard engines.

type klineWindow struct {
	Symbol   string
	Interval string
	Horizon  string
	Trend    string
	CSV      string
	Bars     []market.Candle
}

func buildIntervalRank(intervals []string) map[string]int {
	if len(intervals) == 0 {
		return nil
	}
	rank := make(map[string]int, len(intervals))
	for idx, iv := range intervals {
		key := strings.ToLower(strings.TrimSpace(iv))
		if key == "" {
			continue
		}
		if _, exists := rank[key]; !exists {
			rank[key] = idx
		}
	}
	return rank
}

func intervalRankValue(iv string, rank map[string]int) int {
	if len(rank) == 0 {
		return 0
	}
	key := strings.ToLower(strings.TrimSpace(iv))
	if val, ok := rank[key]; ok {
		return val
	}
	return len(rank) + 1
}

func parseRecentCandles(raw string, keep int) ([]market.Candle, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || keep <= 0 {
		return nil, nil
	}
	var candles []market.Candle
	if err := json.Unmarshal([]byte(raw), &candles); err != nil {
		return nil, err
	}
	if len(candles) == 0 {
		return nil, nil
	}
	if len(candles) > keep {
		candles = candles[len(candles)-keep:]
	}
	return candles, nil
}

func summarizeImagePayloads(imgs []provider.ImagePayload) []string {
	if len(imgs) == 0 {
		return nil
	}
	out := make([]string, 0, len(imgs))
	for _, img := range imgs {
		var b strings.Builder
		desc := strings.TrimSpace(img.Description)
		if desc == "" {
			desc = "(no description)"
		}
		b.WriteString(desc)
		if data := strings.TrimSpace(img.DataURI); data != "" {
			preview := text.Truncate(data, 512)
			b.WriteString("\nDATA: ")
			b.WriteString(preview)
		}
		out = append(out, b.String())
	}
	return out
}

func logAIInput(kind, providerID, purpose, systemPrompt, userPrompt string, imageNotes []string) {
	if strings.TrimSpace(kind) == "" {
		kind = "unknown"
	}
	logger.LogLLMRequest(kind, strings.TrimSpace(providerID), purpose, systemPrompt, userPrompt, imageNotes, "")
}

func buildPreferenceIndex(pref []string) map[string]int {
	m := make(map[string]int, len(pref))
	for i, p := range pref {
		m[p] = i
	}
	return m
}

func orderOutputsByPreference(outs []ModelOutput, pref []string) []ModelOutput {
	if len(outs) <= 1 || len(pref) == 0 {
		return outs
	}
	idx := buildPreferenceIndex(pref)
	fallback := len(idx) + 1
	sort.SliceStable(outs, func(i, j int) bool {
		ri := fallback
		if v, ok := idx[outs[i].ProviderID]; ok {
			ri = v
		}
		rj := fallback
		if v, ok := idx[outs[j].ProviderID]; ok {
			rj = v
		}
		if ri != rj {
			return ri < rj
		}
		return outs[i].ProviderID < outs[j].ProviderID
	})
	return outs
}
