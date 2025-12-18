package decision

import (
	"sort"
	"strings"
)

func mergeMetaVoteBreakdowns(dst, src *MetaVoteBreakdown) *MetaVoteBreakdown {
	if src == nil {
		return dst
	}
	if dst == nil {
		return cloneMetaVoteBreakdown(src)
	}

	if src.TotalWeight > dst.TotalWeight {
		dst.TotalWeight = src.TotalWeight
	}
	if src.Threshold > dst.Threshold {
		dst.Threshold = src.Threshold
	}
	if src.GlobalHoldWeight > dst.GlobalHoldWeight {
		dst.GlobalHoldWeight = src.GlobalHoldWeight
	}

	if len(src.Symbols) == 0 {
		return dst
	}

	index := make(map[string]int, len(dst.Symbols))
	for i := range dst.Symbols {
		key := normalizeMetaSymbolKey(dst.Symbols[i].Symbol)
		if key == "" {
			continue
		}
		index[key] = i
	}

	for _, sym := range src.Symbols {
		key := normalizeMetaSymbolKey(sym.Symbol)
		if key == "" {
			continue
		}
		if i, ok := index[key]; ok {
			dst.Symbols[i] = mergeMetaSymbolBreakdown(dst.Symbols[i], sym)
			continue
		}
		dst.Symbols = append(dst.Symbols, cloneMetaSymbolBreakdown(sym))
		index[key] = len(dst.Symbols) - 1
	}

	sort.SliceStable(dst.Symbols, func(i, j int) bool {
		return normalizeMetaSymbolKey(dst.Symbols[i].Symbol) < normalizeMetaSymbolKey(dst.Symbols[j].Symbol)
	})
	return dst
}

func normalizeMetaSymbolKey(sym string) string {
	return strings.ToUpper(strings.TrimSpace(sym))
}

func cloneMetaVoteBreakdown(src *MetaVoteBreakdown) *MetaVoteBreakdown {
	if src == nil {
		return nil
	}
	out := &MetaVoteBreakdown{
		Threshold:        src.Threshold,
		TotalWeight:      src.TotalWeight,
		GlobalHoldWeight: src.GlobalHoldWeight,
	}
	if len(src.Symbols) > 0 {
		out.Symbols = make([]MetaSymbolBreakdown, 0, len(src.Symbols))
		for _, sym := range src.Symbols {
			out.Symbols = append(out.Symbols, cloneMetaSymbolBreakdown(sym))
		}
	}
	return out
}

func cloneMetaSymbolBreakdown(src MetaSymbolBreakdown) MetaSymbolBreakdown {
	out := MetaSymbolBreakdown{
		Symbol:      src.Symbol,
		FinalAction: src.FinalAction,
	}
	if len(src.Votes) > 0 {
		out.Votes = append([]MetaActionVote(nil), src.Votes...)
	}
	if len(src.Providers) > 0 {
		out.Providers = make([]MetaProviderActions, len(src.Providers))
		for i, p := range src.Providers {
			out.Providers[i] = MetaProviderActions{
				ProviderID: p.ProviderID,
				Weight:     p.Weight,
				Actions:    append([]string(nil), p.Actions...),
			}
		}
	}
	return out
}

func mergeMetaSymbolBreakdown(dst, src MetaSymbolBreakdown) MetaSymbolBreakdown {
	srcFinal := strings.TrimSpace(src.FinalAction)
	dstFinal := strings.TrimSpace(dst.FinalAction)
	if srcFinal != "" && (dstFinal == "" || strings.EqualFold(dstFinal, "HOLD")) {
		dst.FinalAction = srcFinal
	}

	dst.Votes = mergeMetaActionVotes(dst.Votes, src.Votes)
	dst.Providers = mergeMetaProviderActions(dst.Providers, src.Providers)
	return dst
}

func mergeMetaActionVotes(dst, src []MetaActionVote) []MetaActionVote {
	if len(src) == 0 {
		return dst
	}
	if len(dst) == 0 {
		return append([]MetaActionVote(nil), src...)
	}

	weights := make(map[string]float64, len(dst)+len(src))
	for _, v := range dst {
		act := NormalizeAction(v.Action)
		if act == "" {
			continue
		}
		if v.Weight > weights[act] {
			weights[act] = v.Weight
		}
	}
	for _, v := range src {
		act := NormalizeAction(v.Action)
		if act == "" {
			continue
		}
		if v.Weight > weights[act] {
			weights[act] = v.Weight
		}
	}

	out := make([]MetaActionVote, 0, len(weights))
	for act, w := range weights {
		out = append(out, MetaActionVote{Action: act, Weight: w})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Weight != out[j].Weight {
			return out[i].Weight > out[j].Weight
		}
		return out[i].Action < out[j].Action
	})
	return out
}

func mergeMetaProviderActions(dst, src []MetaProviderActions) []MetaProviderActions {
	if len(src) == 0 {
		return dst
	}
	if len(dst) == 0 {
		return cloneMetaProviderActions(src)
	}

	merged := make(map[string]MetaProviderActions, len(dst)+len(src))
	apply := func(items []MetaProviderActions) {
		for _, p := range items {
			id := strings.TrimSpace(p.ProviderID)
			if id == "" {
				continue
			}
			entry := merged[id]
			if entry.ProviderID == "" {
				entry.ProviderID = id
			}
			if p.Weight > entry.Weight {
				entry.Weight = p.Weight
			}
			entry.Actions = mergeProviderActionList(entry.Actions, p.Actions)
			merged[id] = entry
		}
	}
	apply(dst)
	apply(src)

	out := make([]MetaProviderActions, 0, len(merged))
	for _, p := range merged {
		sort.Strings(p.Actions)
		out = append(out, p)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].ProviderID < out[j].ProviderID
	})
	return out
}

func cloneMetaProviderActions(src []MetaProviderActions) []MetaProviderActions {
	if len(src) == 0 {
		return nil
	}
	out := make([]MetaProviderActions, len(src))
	for i, p := range src {
		out[i] = MetaProviderActions{
			ProviderID: p.ProviderID,
			Weight:     p.Weight,
			Actions:    append([]string(nil), p.Actions...),
		}
	}
	return out
}

func mergeProviderActionList(dst, src []string) []string {
	if len(src) == 0 {
		return dst
	}
	if len(dst) == 0 {
		return append([]string(nil), src...)
	}

	set := make(map[string]struct{}, len(dst)+len(src))
	for _, raw := range dst {
		act := NormalizeAction(raw)
		if act == "" {
			continue
		}
		set[act] = struct{}{}
	}
	for _, raw := range src {
		act := NormalizeAction(raw)
		if act == "" {
			continue
		}
		set[act] = struct{}{}
	}

	out := make([]string, 0, len(set))
	for act := range set {
		out = append(out, act)
	}
	sort.Strings(out)
	return out
}

