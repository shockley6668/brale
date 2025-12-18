package database

import "brale/internal/store/decisionlog"

type (
	DecisionLogStore        = decisionlog.DecisionLogStore
	DecisionLogRecord       = decisionlog.DecisionLogRecord
	ImageAttachment         = decisionlog.ImageAttachment
	LiveDecisionTrace       = decisionlog.LiveDecisionTrace
	LiveDecisionStep        = decisionlog.LiveDecisionStep
	LiveDecisionQuery       = decisionlog.LiveDecisionQuery
	StrategyStatus          = decisionlog.StrategyStatus
	StrategyInstanceRecord  = decisionlog.StrategyInstanceRecord
	StrategyChangeLogRecord = decisionlog.StrategyChangeLogRecord
	DecisionRoundSummary    = decisionlog.DecisionRoundSummary
)

var (
	NewDecisionLogObserver = decisionlog.NewDecisionLogObserver
)

const (
	StrategyStatusWaiting StrategyStatus = decisionlog.StrategyStatusWaiting
	StrategyStatusPending StrategyStatus = decisionlog.StrategyStatusPending
	StrategyStatusDone    StrategyStatus = decisionlog.StrategyStatusDone
	StrategyStatusPaused  StrategyStatus = decisionlog.StrategyStatusPaused
)

func NewDecisionLogStore(path string) (*DecisionLogStore, error) {
	return decisionlog.NewDecisionLogStore(path)
}

func EncodeParams(params map[string]any) string {
	return decisionlog.EncodeParams(params)
}

func BuildLiveDecisionTraces(records []DecisionLogRecord) []LiveDecisionTrace {
	return decisionlog.BuildLiveDecisionTraces(records)
}

func BuildDecisionRoundSummaries(finals []DecisionLogRecord, traceLogs map[string][]DecisionLogRecord) []DecisionRoundSummary {
	return decisionlog.BuildDecisionRoundSummaries(finals, traceLogs)
}
