package decision

import (
	"context"
)

// StandardEngine implements a modular decision engine.
// It orchestrates the decision process by coordinating Composer, Dispatcher, and Parser.
type StandardEngine struct {
	Composer   *Composer
	Dispatcher *Dispatcher
	Parser     *Parser
	Aggregator Aggregator
	Observer   DecisionObserver
}

// Ensure StandardEngine implements Decider interface
var _ Decider = (*StandardEngine)(nil)

func NewStandardEngine(c *Composer, d *Dispatcher, p *Parser) *StandardEngine {
	return &StandardEngine{
		Composer:   c,
		Dispatcher: d,
		Parser:     p,
		Aggregator: FirstWinsAggregator{}, // Default
	}
}

func (e *StandardEngine) SetAggregator(agg Aggregator) {
	if agg != nil {
		e.Aggregator = agg
	}
}

func (e *StandardEngine) SetObserver(obs DecisionObserver) {
	e.Observer = obs
}

func (e *StandardEngine) Decide(ctx context.Context, input Context) (DecisionResult, error) {
	// 1. Compose Prompts
	sys, usr, err := e.Composer.Compose(ctx, input)
	if err != nil {
		return DecisionResult{}, err
	}

	// 2. Dispatch to Models
	images := e.Composer.CollectImages(input.Analysis)
	outputs := e.Dispatcher.Dispatch(ctx, sys, usr, images)

	// 3. Parse Outputs
	for i := range outputs {
		if outputs[i].Err != nil {
			continue
		}
		// Skip empty raw response?
		if outputs[i].Raw == "" {
			continue
		}

		parsed, err := e.Parser.Parse(outputs[i].Raw)
		if err != nil {
			// Attach parse error
			outputs[i].Err = err
		} else {
			outputs[i].Parsed = parsed
		}
	}

	// 4. Aggregate Results
	best, err := e.Aggregator.Aggregate(ctx, outputs)
	if err != nil {
		return DecisionResult{}, err
	}

	result := best.Parsed

	// 5. Post-process (Normalization, Profile Attachment)
	// This logic was in legacy decideSingle. Ideally belongs in Parser or a PostProcessor?
	// For Phase 1, we can keep it here or in Parser.
	result.Decisions = NormalizeAndAlignDecisions(result.Decisions, input.Positions)
	AttachDecisionProfiles(result.Decisions, input.FeatureReports)

	return result, nil
}
