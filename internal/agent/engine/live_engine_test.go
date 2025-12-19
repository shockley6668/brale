package engine

import (
	"context"
	"testing"

	"brale/internal/agent/interfaces"
	"brale/internal/config"
	"brale/internal/decision"
	"brale/internal/gateway/exchange"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type MockPosService struct {
	mock.Mock
}

func (m *MockPosService) GetAccountSnapshot(ctx context.Context) (decision.AccountSnapshot, error) {
	args := m.Called(ctx)
	return args.Get(0).(decision.AccountSnapshot), args.Error(1)
}
func (m *MockPosService) ListPositions(ctx context.Context) ([]decision.PositionSnapshot, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]decision.PositionSnapshot), args.Error(1)
}
func (m *MockPosService) SyncStrategies(context.Context, exchange.PlanUpdateHook) error { return nil }
func (m *MockPosService) TradeIDForSymbol(string) (int, bool)                           { return 0, false }
func (m *MockPosService) ExecuteDecision(ctx context.Context, traceID string, d decision.Decision, price float64) error {
	args := m.Called(ctx, traceID, d, price)
	return args.Error(0)
}

type MockMktService struct {
	mock.Mock
}

func (m *MockMktService) GetAnalysisContexts(ctx context.Context, symbols []string) ([]decision.AnalysisContext, error) {
	args := m.Called(ctx, symbols)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]decision.AnalysisContext), args.Error(1)
}
func (m *MockMktService) CaptureIndicators(data []decision.AnalysisContext) {
	m.Called(data)
}
func (m *MockMktService) LatestPrice(ctx context.Context, symbol string) float64 {
	args := m.Called(ctx, symbol)
	return args.Get(0).(float64)
}
func (m *MockMktService) GetATR(symbol string) (float64, bool) {
	args := m.Called(symbol)
	return args.Get(0).(float64), args.Bool(1)
}

type MockDecider struct {
	mock.Mock
}

func (m *MockDecider) Decide(ctx context.Context, input decision.Context) (decision.DecisionResult, error) {
	args := m.Called(ctx, input)
	return args.Get(0).(decision.DecisionResult), args.Error(1)
}

type MockPlanScheduler struct {
	mock.Mock
}

func (m *MockPlanScheduler) AdjustPlan(ctx context.Context, spec interfaces.PlanAdjustSpec) error {
	args := m.Called(ctx, spec)
	return args.Error(0)
}
func (m *MockPlanScheduler) ProcessUpdateDecision(ctx context.Context, traceID string, d decision.Decision) error {
	args := m.Called(ctx, traceID, d)
	return args.Error(0)
}

func TestLiveEngine_RunCycle(t *testing.T) {
	mockPos := new(MockPosService)
	mockMkt := new(MockMktService)
	mockDecider := new(MockDecider)

	cfg := &config.Config{}
	p := EngineParams{
		Config:     cfg,
		PosService: mockPos,
		MktService: mockMkt,
		Decider:    mockDecider,
	}
	engine := NewLiveEngine(p)

	ctx := context.Background()
	symbols := []string{"BTC/USDT"}

	mockPos.On("GetAccountSnapshot", ctx).Return(decision.AccountSnapshot{}, nil)
	mockPos.On("ListPositions", ctx).Return([]decision.PositionSnapshot{}, nil)
	mockMkt.On("GetAnalysisContexts", ctx, symbols).Return([]decision.AnalysisContext{}, nil)
	mockMkt.On("CaptureIndicators", mock.Anything).Return()

	decResult := decision.DecisionResult{
		TraceID: "test-trace",
		Decisions: []decision.Decision{
			{Symbol: "BTC/USDT", Action: "open_long", Leverage: 0, PositionSizeUSD: 0},
		},
	}
	mockDecider.On("Decide", ctx, mock.Anything).Return(decResult, nil)

	mockMkt.On("LatestPrice", ctx, "BTC/USDT").Return(50000.0)
	mockPos.On("ExecuteDecision", ctx, "test-trace", mock.AnythingOfType("decision.Decision"), 50000.0).Return(nil)

	err := engine.RunCycle(ctx, symbols)
	assert.NoError(t, err)

	mockPos.AssertExpectations(t)
	mockMkt.AssertExpectations(t)
	mockDecider.AssertExpectations(t)
}

func TestLiveEngine_UpdateExitPlan(t *testing.T) {
	mockScheduler := new(MockPlanScheduler)

	p := EngineParams{
		Config:        &config.Config{},
		PlanScheduler: mockScheduler,
	}
	engine := NewLiveEngine(p)

	ctx := context.Background()

	mockScheduler.On("ProcessUpdateDecision", ctx, "test-trace-update", mock.MatchedBy(func(d decision.Decision) bool {
		return d.Action == "update_exit_plan" && d.ExitPlan != nil && d.ExitPlan.ID == "plan_test"
	})).Return(nil)

	accepted := engine.executeDecisions(ctx, []decision.Decision{
		{
			Symbol:   "BTC/USDT",
			Action:   "update_exit_plan",
			ExitPlan: &decision.ExitPlanSpec{ID: "plan_test"},
		},
	}, "test-trace-update")
	assert.Len(t, accepted, 1)

	mockScheduler.AssertExpectations(t)
}
