package decision

import (
	"sort"
	"strings"
)

func buildMetaVoteBreakdown(
	votes map[string]map[string]float64,
	details map[string]map[string][]metaChoice,
	threshold float64,
	totalWeight float64,
) *MetaVoteBreakdown {
	if len(votes) == 0 {
		return nil
	}

	bd := &MetaVoteBreakdown{Threshold: threshold, TotalWeight: totalWeight}
	bd.GlobalHoldWeight = globalHoldWeight(votes)

	syms := metaSymbolsFromVotes(votes)
	bd.Symbols = make([]MetaSymbolBreakdown, 0, len(syms))
	for _, sym := range syms {
		actVotes := votes[sym]
		if len(actVotes) == 0 {
			continue
		}

		displaySym := strings.ToUpper(strings.TrimSpace(sym))
		vlist := metaActionVoteList(actVotes)
		providers := metaProviderActionsList(details[sym])
		finalAction := metaFinalAction(vlist, threshold)

		bd.Symbols = append(bd.Symbols, MetaSymbolBreakdown{
			Symbol:      displaySym,
			FinalAction: finalAction,
			Votes:       vlist,
			Providers:   providers,
		})
	}

	return bd
}

func globalHoldWeight(votes map[string]map[string]float64) float64 {
	hv := votes[holdSymbolKey]
	if len(hv) == 0 {
		return 0
	}
	return hv["hold"]
}

func metaSymbolsFromVotes(votes map[string]map[string]float64) []string {
	if len(votes) == 0 {
		return nil
	}
	syms := make([]string, 0, len(votes))
	for sym := range votes {
		if sym == holdSymbolKey {
			continue
		}
		if strings.TrimSpace(sym) == "" {
			continue
		}
		syms = append(syms, sym)
	}
	sort.Strings(syms)
	return syms
}

func metaActionVoteList(actVotes map[string]float64) []MetaActionVote {
	if len(actVotes) == 0 {
		return nil
	}
	vlist := make([]MetaActionVote, 0, len(actVotes))
	for act, w := range actVotes {
		act = NormalizeAction(act)
		if act == "" {
			continue
		}
		vlist = append(vlist, MetaActionVote{Action: act, Weight: w})
	}
	sort.SliceStable(vlist, func(i, j int) bool {
		if vlist[i].Weight != vlist[j].Weight {
			return vlist[i].Weight > vlist[j].Weight
		}
		return vlist[i].Action < vlist[j].Action
	})
	return vlist
}

func metaProviderActionsList(symDetails map[string][]metaChoice) []MetaProviderActions {
	if len(symDetails) == 0 {
		return nil
	}
	pmap := make(map[string]*MetaProviderActions)
	for act, choices := range symDetails {
		act = NormalizeAction(act)
		if act == "" {
			continue
		}
		for _, c := range choices {
			id := strings.TrimSpace(c.ID)
			if id == "" {
				continue
			}
			entry := pmap[id]
			if entry == nil {
				entry = &MetaProviderActions{ProviderID: id, Weight: c.Weight}
				pmap[id] = entry
			} else if entry.Weight == 0 {
				entry.Weight = c.Weight
			}
			appendUniqueString(&entry.Actions, act)
		}
	}

	providers := make([]MetaProviderActions, 0, len(pmap))
	for _, v := range pmap {
		sort.Strings(v.Actions)
		providers = append(providers, *v)
	}
	sort.SliceStable(providers, func(i, j int) bool { return providers[i].ProviderID < providers[j].ProviderID })
	return providers
}

func metaFinalAction(vlist []MetaActionVote, threshold float64) string {
	finalAction := "HOLD"
	for _, v := range vlist {
		if v.Action != "hold" && v.Weight >= threshold {
			finalAction = strings.ToUpper(v.Action)
			break
		}
	}
	return finalAction
}

func appendUniqueString(dst *[]string, value string) {
	if dst == nil {
		return
	}
	for _, existing := range *dst {
		if existing == value {
			return
		}
	}
	*dst = append(*dst, value)
}
