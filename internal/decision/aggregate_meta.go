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

type MetaAggregator struct {
	Weights    map[string]float64
	Preference []string
}

type metaChoice struct {
	ID       string
	Decision Decision
	Weight   float64
}

func (a MetaAggregator) Name() string { return "meta" }

const holdSymbolKey = "__META_HOLD__"

// Aggregate combines output from multiple LLM providers into a single "meta" decision.
//
// Logic:
// 1. Collects votes: Each provider votes for an Action (Buy/Sell/Hold) on a Symbol.
// 2. Weights votes: Applies configured weights (e.g., GPT-4=2.0, Local=1.0).
// 3. Determines Consensus:
//   - Calculates a dynamic threshold (default 2/3 of total weight).
//   - If any Action > Threshold, it wins.
//   - If "Hold" > Threshold, we hold.
//   - If no strong consensus, falls back to Hold.
//
// 4. Resolves Details: For the winning action, picks the specific plan/reasoning from the highest-ranked provider.
func (a MetaAggregator) Aggregate(ctx context.Context, outputs []ModelOutput) (ModelOutput, error) {
	prefIndex := buildPreferenceIndex(a.Preference)
	tally := a.tallyMetaVotes(outputs)
	if len(tally.votes) == 0 || tally.totalWeight == 0 {
		return ModelOutput{}, errors.New("无可用的模型输出")
	}
	threshold := computeThreshold(tally.totalWeight)
	breakdown := buildMetaVoteBreakdown(tally.votes, tally.details, threshold, tally.totalWeight)

	if shouldHoldConsensus(tally.votes, threshold) {
		holdDecision, winners := buildHoldDecision(outputs, tally.votes, tally.details, threshold, tally.totalWeight, prefIndex)
		res := DecisionResult{Decisions: []Decision{holdDecision}, MetaSummary: "", MetaBreakdown: breakdown}
		return buildMetaOutput(outputs, res, winners, prefIndex), nil
	}

	decisions, winners := collectExecutableDecisions(tally.votes, tally.details, threshold, prefIndex)
	if len(decisions) == 0 {

		holdDecision, winners := buildHoldDecision(outputs, tally.votes, tally.details, threshold, tally.totalWeight, prefIndex)
		res := DecisionResult{Decisions: []Decision{holdDecision}, MetaSummary: "", MetaBreakdown: breakdown}
		return buildMetaOutput(outputs, res, winners, prefIndex), nil
	}
	res := DecisionResult{Decisions: decisions, MetaSummary: "", MetaBreakdown: breakdown}
	return buildMetaOutput(outputs, res, winners, prefIndex), nil
}

type metaTally struct {
	votes       map[string]map[string]float64
	details     map[string]map[string][]metaChoice
	totalWeight float64
}

func (a MetaAggregator) tallyMetaVotes(outputs []ModelOutput) metaTally {
	tally := metaTally{
		votes:   map[string]map[string]float64{},
		details: map[string]map[string][]metaChoice{},
	}
	seen := map[string]map[string]bool{}

	for _, o := range outputs {
		if o.Err != nil || len(o.Parsed.Decisions) == 0 {
			continue
		}
		w := a.providerWeight(o.ProviderID)
		if seen[o.ProviderID] == nil {
			seen[o.ProviderID] = map[string]bool{}
		}

		added := tallyProviderVotes(&tally, o, w, seen[o.ProviderID])
		if added {
			tally.totalWeight += w
		}
	}
	return tally
}

func (a MetaAggregator) providerWeight(providerID string) float64 {
	w := 1.0
	if a.Weights == nil {
		return w
	}
	if v, ok := a.Weights[providerID]; ok && v > 0 {
		return v
	}
	return w
}

func tallyProviderVotes(tally *metaTally, out ModelOutput, weight float64, seen map[string]bool) bool {
	if tally == nil || len(out.Parsed.Decisions) == 0 {
		return false
	}
	hadAction := false
	for _, d := range out.Parsed.Decisions {
		sym, act, ok := normalizeVoteKey(d)
		if !ok {
			continue
		}
		key := sym + "#" + act
		if seen[key] {
			continue
		}
		seen[key] = true
		hadAction = true

		if tally.votes[sym] == nil {
			tally.votes[sym] = map[string]float64{}
		}
		if tally.details[sym] == nil {
			tally.details[sym] = map[string][]metaChoice{}
		}
		tally.votes[sym][act] += weight
		tally.details[sym][act] = append(tally.details[sym][act], metaChoice{ID: out.ProviderID, Decision: d, Weight: weight})
	}
	return hadAction
}

func normalizeVoteKey(d Decision) (string, string, bool) {
	act := NormalizeAction(d.Action)
	if act == "" {
		return "", "", false
	}
	sym := strings.ToUpper(strings.TrimSpace(d.Symbol))
	if act == "hold" {
		if sym == "" {
			sym = holdSymbolKey
		}
		return sym, act, true
	}
	if sym == "" {
		return "", "", false
	}
	return sym, act, true
}

func shouldHoldConsensus(votes map[string]map[string]float64, threshold float64) bool {
	hv := votes[holdSymbolKey]
	return len(hv) > 0 && hv["hold"] >= threshold
}

func collectExecutableDecisions(votes map[string]map[string]float64, details map[string]map[string][]metaChoice, threshold float64, prefIndex map[string]int) ([]Decision, map[string]float64) {
	syms := make([]string, 0, len(votes))
	for sym := range votes {
		if sym == holdSymbolKey {
			continue
		}
		syms = append(syms, sym)
	}
	sort.Strings(syms)

	decisions := make([]Decision, 0)
	winners := make(map[string]float64)
	for _, sym := range syms {
		decisions = append(decisions, pickExecutableDecisionsForSymbol(sym, votes[sym], details[sym], threshold, prefIndex, winners)...)
	}
	return decisions, winners
}

func pickExecutableDecisionsForSymbol(sym string, symVotes map[string]float64, symDetails map[string][]metaChoice, threshold float64, prefIndex map[string]int, winners map[string]float64) []Decision {
	if len(symVotes) == 0 {
		return nil
	}
	actionNames := make([]string, 0, len(symVotes))
	for act := range symVotes {
		actionNames = append(actionNames, act)
	}
	sort.Strings(actionNames)

	out := make([]Decision, 0)
	for _, act := range actionNames {
		weight := symVotes[act]
		if weight < threshold || act == "hold" {
			continue
		}
		choices := symDetails[act]
		choice := pickDecision(choices, act, sym, prefIndex)
		out = append(out, choice)
		addWinnersForAction(winners, choices, act)
	}
	return out
}

func addWinnersForAction(winners map[string]float64, choices []metaChoice, act string) {
	if winners == nil || len(choices) == 0 {
		return
	}
	for _, c := range choices {
		if NormalizeAction(c.Decision.Action) == act {
			winners[c.ID] += c.Weight
		}
	}
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

// pickMajorityChoice identifies if any symbol+action pair has achieved strict majority.
// Returns the symbol, action, and the specific decision content from the preferred provider.
//
// Returns:
// - symbol, action, providerID, decision, success(bool)
func pickMajorityChoice(votes map[string]map[string]float64, details map[string]map[string][]metaChoice, pref map[string]int) (string, string, string, Decision, bool) {
	if len(votes) == 0 || len(details) == 0 {
		return "", "", "", Decision{}, false
	}

	targetSym := chooseTargetSymbol(votes)
	if targetSym == "" {
		return "", "", "", Decision{}, false
	}

	majorAct := chooseMajorityAction(votes[targetSym])
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

// chooseTargetSymbol picks the symbol with the strongest aggregate weight.
// Tie-breaker: pick lexicographically smaller symbol for deterministic output.
func chooseTargetSymbol(votes map[string]map[string]float64) string {
	if len(votes) == 0 {
		return ""
	}
	syms := make([]string, 0, len(votes))
	for sym := range votes {
		if sym == holdSymbolKey {
			continue
		}
		syms = append(syms, sym)
	}
	sort.Strings(syms)

	switch {
	case len(syms) == 1:
		return syms[0]
	case votes[holdSymbolKey] != nil:
		// If any hold votes exist, prioritize checking hold consensus first.
		return holdSymbolKey
	default:
		bestTotal := -1.0
		target := ""
		for _, sym := range syms {
			sum := 0.0
			for _, w := range votes[sym] {
				sum += w
			}
			if target == "" || sum > bestTotal || (sum == bestTotal && sym < target) {
				target = sym
				bestTotal = sum
			}
		}
		return target
	}
}

// chooseMajorityAction picks the action with the highest weight for the target symbol.
// Tie-breaker: lexical order for determinism.
func chooseMajorityAction(actions map[string]float64) string {
	if len(actions) == 0 {
		return ""
	}
	majorAct := ""
	majorWeight := -1.0
	for act, w := range actions {
		if majorAct == "" || w > majorWeight || (w == majorWeight && act < majorAct) {
			majorAct = act
			majorWeight = w
		}
	}
	return majorAct
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

// computeThreshold calculates the required vote weight for a decision to pass.
//
// Logic:
// - Uses a super-majority rule (2/3 of total active weight).
// - Enforces a minimum floor of 50%.
// - This prevents weak consensus decisions from executing trades.
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
