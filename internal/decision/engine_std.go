package decision

import (
	"context"
)

type StandardEngine struct {
	Composer   *Composer
	Dispatcher *Dispatcher
	Parser     *Parser
	Aggregator Aggregator
	Observer   DecisionObserver
}

var _ Decider = (*StandardEngine)(nil)

func NewStandardEngine(c *Composer, d *Dispatcher, p *Parser) *StandardEngine {
	return &StandardEngine{
		Composer:   c,
		Dispatcher: d,
		Parser:     p,
		Aggregator: FirstWinsAggregator{},
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

	sys, usr, err := e.Composer.Compose(ctx, input)
	if err != nil {
		return DecisionResult{}, err
	}

	images := e.Composer.CollectImages(input.Analysis)
	outputs := e.Dispatcher.Dispatch(ctx, sys, usr, images)

	for i := range outputs {
		if outputs[i].Err != nil {
			continue
		}

		if outputs[i].Raw == "" {
			continue
		}

		parsed, err := e.Parser.Parse(outputs[i].Raw)
		if err != nil {

			outputs[i].Err = err
		} else {
			outputs[i].Parsed = parsed
		}
	}

	best, err := e.Aggregator.Aggregate(ctx, outputs)
	if err != nil {
		return DecisionResult{}, err
	}

	result := best.Parsed

	result.Decisions = NormalizeAndAlignDecisions(result.Decisions, input.Positions)
	AttachDecisionProfiles(result.Decisions, input.FeatureReports)

	return result, nil
}
