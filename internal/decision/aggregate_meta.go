package decision

import (
	"context"
	"errors"
	"math"
	"math/rand"
	"sort"
	"strings"
	"time"
)

// MetaAggregator：逐币种多数决（留权重，默认相等）。
// 规则：
// - 每个模型可以返回多个动作，针对同一 symbol+action 仅记一票
// - 对 symbol+action 进行加权投票，权重达到 2/3（默认）阈值才执行；不足则忽略
// - hold 默认按 symbol 计票；若模型返回不带 symbol 的 hold，视为“整轮观望”
// - MetaSummary 会列出各 symbol 各动作的票数，方便查看分歧
type MetaAggregator struct {
	Weights    map[string]float64
	Preference []string // 权重/动作相同时用于决策与原文选择的优先级
}

type metaChoice struct {
	ID       string
	Decision Decision
	Weight   float64
}

func (a MetaAggregator) Name() string { return "meta" }

const holdSymbolKey = "__META_HOLD__"

func (a MetaAggregator) Aggregate(ctx context.Context, outputs []ModelOutput) (ModelOutput, error) {
	votes := map[string]map[string]float64{}        // symbol -> action -> weight
	details := map[string]map[string][]metaChoice{} // symbol -> action -> choices
	seen := map[string]map[string]bool{}            // provider -> sym#act -> seen?
	totalWeight := 0.0                              // 累积参与投票的权重
	prefIndex := buildPreferenceIndex(a.Preference)
	for _, o := range outputs {
		if o.Err != nil || len(o.Parsed.Decisions) == 0 {
			continue
		}
		w := 1.0
		if a.Weights != nil {
			if v, ok := a.Weights[o.ProviderID]; ok && v > 0 {
				w = v
			}
		}
		if seen[o.ProviderID] == nil {
			seen[o.ProviderID] = map[string]bool{}
		}
		hadAction := false
		for _, d := range o.Parsed.Decisions {
			act := NormalizeAction(d.Action)
			if act == "" {
				continue
			}
			sym := strings.ToUpper(strings.TrimSpace(d.Symbol))
			if act == "hold" {
				// 兼容两种 hold：
				// 1) 带 symbol：仅表示该 symbol 观望（不应跨币种累加）
				// 2) 不带 symbol：表示整轮观望（全局 hold）
				if sym == "" {
					sym = holdSymbolKey
				}
			} else if sym == "" {
				continue
			}
			key := sym + "#" + act
			if seen[o.ProviderID][key] {
				continue
			}
			seen[o.ProviderID][key] = true
			hadAction = true
			if _, ok := votes[sym]; !ok {
				votes[sym] = map[string]float64{}
			}
			if _, ok := details[sym]; !ok {
				details[sym] = map[string][]metaChoice{}
			}
			votes[sym][act] += w
			details[sym][act] = append(details[sym][act], metaChoice{ID: o.ProviderID, Decision: d, Weight: w})
		}
		if hadAction {
			totalWeight += w
		}
	}
	if len(votes) == 0 || totalWeight == 0 {
		return ModelOutput{}, errors.New("无可用的模型输出")
	}
	threshold := computeThreshold(totalWeight)
	breakdown := buildMetaVoteBreakdown(votes, details, threshold, totalWeight)

	// 若 hold 票达到阈值，则整轮观望
	if hv, ok := votes[holdSymbolKey]; ok {
		if hv["hold"] >= threshold {
			holdDecision, winners := buildHoldDecision(outputs, votes, details, threshold, totalWeight, prefIndex)
			res := DecisionResult{
				Decisions:     []Decision{holdDecision},
				MetaSummary:   "", // Removed unused summary
				MetaBreakdown: breakdown,
			}
			return buildMetaOutput(outputs, res, winners, prefIndex), nil
		}
	}

	decisions := make([]Decision, 0)
	winners := make(map[string]float64)
	syms := make([]string, 0, len(votes))
	for sym := range votes {
		if sym == holdSymbolKey {
			continue
		}
		syms = append(syms, sym)
	}
	sort.Strings(syms)
	for _, sym := range syms {
		actionNames := make([]string, 0, len(votes[sym]))
		for act := range votes[sym] {
			actionNames = append(actionNames, act)
		}
		sort.Strings(actionNames)
		for _, act := range actionNames {
			weight := votes[sym][act]
			if weight < threshold || act == "hold" {
				continue
			}
			choice := pickDecision(details[sym][act], act, sym, prefIndex)
			decisions = append(decisions, choice)
			for _, c := range details[sym][act] {
				if NormalizeAction(c.Decision.Action) == act {
					winners[c.ID] += c.Weight
				}
			}
		}
	}
	if len(decisions) == 0 {
		// 没有任何动作满足阈值，退回 hold
		holdDecision, winners := buildHoldDecision(outputs, votes, details, threshold, totalWeight, prefIndex)
		res := DecisionResult{
			Decisions:     []Decision{holdDecision},
			MetaSummary:   "", // Removed unused summary
			MetaBreakdown: breakdown,
		}
		return buildMetaOutput(outputs, res, winners, prefIndex), nil
	}
	res := DecisionResult{Decisions: decisions, MetaSummary: "", MetaBreakdown: breakdown}
	return buildMetaOutput(outputs, res, winners, prefIndex), nil
}

func buildHoldDecision(outputs []ModelOutput, votes map[string]map[string]float64, details map[string]map[string][]metaChoice, threshold, totalWeight float64, pref map[string]int) (Decision, map[string]float64) {
	hold := Decision{Action: "hold"}

	sym, act, providerID, choice, ok := pickMajorityChoice(votes, details, pref)
	if !ok {
		hold.Reasoning = "未达执行阈值，保持观望。"
		return hold, map[string]float64{}
	}

	reasoning := strings.TrimSpace(choice.Reasoning)
	if reasoning == "" && providerID != "" {
		if out := findProviderOutput(outputs, providerID); out != nil {
			reasoning = summarizeRawOutput(out.Raw)
		}
	}
	if reasoning == "" {
		reasoning = "未达执行阈值，保持观望。"
	}

	// 当最终为 HOLD 时，为避免误解，明确说明“多数倾向”来自哪个动作。
	// 对于多 symbol 的情况，追加 symbol 仅用于日志展示；实际执行仍为 HOLD。
	if act != "" && act != "hold" {
		prefix := "未达执行阈值，保持观望。多数倾向: " + strings.ToUpper(act)
		if sym != "" && sym != holdSymbolKey {
			prefix += " (" + sym + ")"
		}
		reasoning = prefix + "\n" + reasoning
	}
	hold.Reasoning = reasoning

	if providerID == "" {
		return hold, map[string]float64{}
	}
	return hold, map[string]float64{providerID: 1}
}

func pickMajorityChoice(votes map[string]map[string]float64, details map[string]map[string][]metaChoice, pref map[string]int) (string, string, string, Decision, bool) {
	if len(votes) == 0 || len(details) == 0 {
		return "", "", "", Decision{}, false
	}

	syms := make([]string, 0, len(votes))
	for sym := range votes {
		if sym == holdSymbolKey {
			continue
		}
		syms = append(syms, sym)
	}
	sort.Strings(syms)

	targetSym := ""
	switch {
	case len(syms) == 1:
		targetSym = syms[0]
	case votes[holdSymbolKey] != nil:
		targetSym = holdSymbolKey
	default:
		bestTotal := -1.0
		for sym, actions := range votes {
			if sym == holdSymbolKey {
				continue
			}
			sum := 0.0
			for _, w := range actions {
				sum += w
			}
			if targetSym == "" || sum > bestTotal || (sum == bestTotal && sym < targetSym) {
				targetSym = sym
				bestTotal = sum
			}
		}
	}
	if targetSym == "" {
		return "", "", "", Decision{}, false
	}

	actions := votes[targetSym]
	if len(actions) == 0 {
		return "", "", "", Decision{}, false
	}
	majorAct := ""
	majorWeight := -1.0
	for act, w := range actions {
		if majorAct == "" || w > majorWeight || (w == majorWeight && act < majorAct) {
			majorAct = act
			majorWeight = w
		}
	}
	if majorAct == "" {
		return "", "", "", Decision{}, false
	}

	choices := details[targetSym][majorAct]
	if len(choices) == 0 {
		return targetSym, majorAct, "", Decision{}, false
	}
	providerID, bestDecision := pickPreferredDecision(choices, pref)
	if providerID == "" {
		return targetSym, majorAct, "", Decision{}, false
	}
	return targetSym, majorAct, providerID, bestDecision, true
}

func pickPreferredDecision(choices []metaChoice, pref map[string]int) (string, Decision) {
	type agg struct {
		weight   float64
		decision Decision
		hasDec   bool
	}
	byProvider := make(map[string]*agg)
	for _, c := range choices {
		id := strings.TrimSpace(c.ID)
		if id == "" {
			continue
		}
		item := byProvider[id]
		if item == nil {
			item = &agg{}
			byProvider[id] = item
		}
		item.weight += c.Weight
		if !item.hasDec || (strings.TrimSpace(item.decision.Reasoning) == "" && strings.TrimSpace(c.Decision.Reasoning) != "") {
			item.decision = c.Decision
			item.hasDec = true
		}
	}
	if len(byProvider) == 0 {
		return "", Decision{}
	}
	bestID := ""
	bestRank := len(pref) + 1
	bestWeight := -1.0
	for id, item := range byProvider {
		rank := len(pref) + 1
		if r, ok := pref[id]; ok {
			rank = r
		}
		if bestID == "" || rank < bestRank || (rank == bestRank && (item.weight > bestWeight || (item.weight == bestWeight && id < bestID))) {
			bestID = id
			bestRank = rank
			bestWeight = item.weight
		}
	}
	if bestID == "" {
		return "", Decision{}
	}
	return bestID, byProvider[bestID].decision
}

func summarizeRawOutput(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	raw = strings.ReplaceAll(raw, "\r", "\n")
	lines := strings.Split(raw, "\n")
	best := ""
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		best = line
		break
	}
	if best == "" {
		best = raw
	}
	const maxRunes = 240
	runes := []rune(best)
	if len(runes) > maxRunes {
		best = string(runes[:maxRunes]) + "…"
	}
	return best
}

func pickDecision(choices []metaChoice, action, symbol string, pref map[string]int) Decision {
	act := NormalizeAction(action)
	maxWeight := -1.0
	for _, c := range choices {
		if NormalizeAction(c.Decision.Action) != act {
			continue
		}
		if c.Weight > maxWeight {
			maxWeight = c.Weight
		}
	}
	if maxWeight < 0 {
		return Decision{Symbol: symbol, Action: act}
	}
	bestIdx := -1
	bestRank := len(pref) + 1
	bestProvider := ""
	for i, c := range choices {
		if NormalizeAction(c.Decision.Action) != act || c.Weight != maxWeight {
			continue
		}
		rank := len(pref) + 1
		if v, ok := pref[c.ID]; ok {
			rank = v
		}
		if bestIdx == -1 || rank < bestRank || (rank == bestRank && c.ID < bestProvider) {
			bestIdx = i
			bestRank = rank
			bestProvider = c.ID
		}
	}
	if bestIdx == -1 {
		return Decision{Symbol: symbol, Action: act}
	}
	dup := choices[bestIdx].Decision
	dup.Symbol = symbol
	dup.Action = act
	return dup
}

func pickWeightedProvider(weights map[string]float64, pref map[string]int) string {
	total := 0.0
	ids := make([]string, 0, len(weights))
	for id, w := range weights {
		if w <= 0 {
			continue
		}
		total += w
		ids = append(ids, id)
	}
	if total == 0 || len(ids) == 0 {
		return ""
	}
	if len(pref) > 0 {
		bestID := ""
		bestRank := len(pref) + 1
		for id, w := range weights {
			if w <= 0 {
				continue
			}
			if r, ok := pref[id]; ok && r < bestRank {
				bestID = id
				bestRank = r
			}
		}
		if bestID != "" {
			return bestID
		}
	}
	sort.Strings(ids)
	rnd := rand.New(rand.NewSource(time.Now().UnixNano()))
	target := rnd.Float64() * total
	acc := 0.0
	for _, id := range ids {
		w := weights[id]
		if w <= 0 {
			continue
		}
		acc += w
		if target < acc {
			return id
		}
	}
	return ids[len(ids)-1]
}

func findProviderOutput(outputs []ModelOutput, id string) *ModelOutput {
	for i := range outputs {
		if outputs[i].ProviderID == id {
			return &outputs[i]
		}
	}
	return nil
}

func computeThreshold(total float64) float64 {
	if total <= 0 {
		return 0
	}
	val := total * 2.0 / 3.0
	return math.Max(val, total*0.5)
}

func buildMetaOutput(outputs []ModelOutput, res DecisionResult, winners map[string]float64, pref map[string]int) ModelOutput {
	best := ModelOutput{ProviderID: "meta", Parsed: res}
	if len(outputs) == 1 && outputs[0].Err == nil {
		best.Raw = outputs[0].Raw
		best.Parsed.RawJSON = outputs[0].Parsed.RawJSON
		return best
	}
	if id := pickWeightedProvider(winners, pref); id != "" {
		if out := findProviderOutput(outputs, id); out != nil && out.Err == nil && out.Raw != "" {
			best.Raw = out.Raw
			best.Parsed.RawJSON = out.Parsed.RawJSON
		}
	}
	return best
}
