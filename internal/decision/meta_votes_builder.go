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

	bd := &MetaVoteBreakdown{
		Threshold:   threshold,
		TotalWeight: totalWeight,
	}
	if hv, ok := votes[holdSymbolKey]; ok {
		bd.GlobalHoldWeight = hv["hold"]
	}

	syms := make([]string, 0, len(votes))
	for sym := range votes {
		if sym == holdSymbolKey {
			continue
		}
		if strings.TrimSpace(sym) == "" {
			continue
		}
		syms = append(syms, sym) // 保持原始 key 用于 map 查找
	}
	sort.Strings(syms)

	bd.Symbols = make([]MetaSymbolBreakdown, 0, len(syms))
	for _, sym := range syms {
		actVotes := votes[sym]
		if len(actVotes) == 0 {
			continue
		}
		displaySym := strings.ToUpper(strings.TrimSpace(sym)) // 显示用大写

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

		// provider -> actions
		pmap := map[string]*MetaProviderActions{}
		if symDetails := details[sym]; len(symDetails) > 0 {
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
					// de-dup action
					seen := false
					for _, existing := range entry.Actions {
						if existing == act {
							seen = true
							break
						}
					}
					if !seen {
						entry.Actions = append(entry.Actions, act)
					}
				}
			}
		}

		providers := make([]MetaProviderActions, 0, len(pmap))
		for _, v := range pmap {
			sort.Strings(v.Actions)
			providers = append(providers, *v)
		}
		sort.SliceStable(providers, func(i, j int) bool {
			return providers[i].ProviderID < providers[j].ProviderID
		})

		// 判断该币种最终执行的动作：取达到阈值且非 hold 的最高票动作，否则为 HOLD
		finalAction := "HOLD"
		for _, v := range vlist {
			// 只有明确非 hold 且达到阈值才视为 Action
			if v.Action != "hold" && v.Weight >= threshold {
				finalAction = strings.ToUpper(v.Action)
				break
			}
		}

		bd.Symbols = append(bd.Symbols, MetaSymbolBreakdown{
			Symbol:      displaySym,
			FinalAction: finalAction,
			Votes:       vlist,
			Providers:   providers,
		})
	}

	return bd
}
