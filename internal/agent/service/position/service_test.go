package position

import (
	"context"
	"testing"

	"brale/internal/decision"
	"brale/internal/gateway/exchange"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type MockExecutionManager struct {
	mock.Mock
}

func (m *MockExecutionManager) Execute(ctx context.Context, input decision.DecisionInput) error {
	args := m.Called(ctx, input)
	return args.Error(0)
}

func (m *MockExecutionManager) AccountBalance() exchange.Balance {
	return exchange.Balance{}
}

func (m *MockExecutionManager) SetPlanUpdateHook(exchange.PlanUpdateHook) {}

func (m *MockExecutionManager) SyncStrategyPlans(ctx context.Context, tradeID int, plans any) error {
	return nil
}
func (m *MockExecutionManager) CloseFreqtradePosition(ctx context.Context, tradeID int, symbol, side string, ratio float64) error {
	return nil
}
func (m *MockExecutionManager) ManualOpenPosition(ctx context.Context, req exchange.ManualOpenRequest) error {
	return nil
}
func (m *MockExecutionManager) GetLatestPriceQuote(ctx context.Context, symbol string) (exchange.PriceQuote, error) {
	return exchange.PriceQuote{}, nil
}
func (m *MockExecutionManager) RefreshBalance(ctx context.Context) (exchange.Balance, error) {
	return exchange.Balance{}, nil
}
func (m *MockExecutionManager) HandleWebhook(context.Context, exchange.WebhookMessage) {}
func (m *MockExecutionManager) PositionsForAPI(ctx context.Context, opts exchange.PositionListOptions) (exchange.PositionListResult, error) {
	return exchange.PositionListResult{}, nil
}
func (m *MockExecutionManager) ListFreqtradeEvents(ctx context.Context, tradeID, limit int) ([]exchange.TradeEvent, error) {
	return nil, nil
}
func (m *MockExecutionManager) PublishPrice(string, exchange.PriceQuote) {}
func (m *MockExecutionManager) ListOpenPositions(ctx context.Context) ([]exchange.Position, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]exchange.Position), args.Error(1)
}
func (m *MockExecutionManager) TradeIDBySymbol(string) (int, bool) { return 0, false }
func (m *MockExecutionManager) EntryPriceBySymbol(string) float64  { return 0 }
func (m *MockExecutionManager) CacheDecision(key string, _ decision.Decision) string {
	return key
}
func (m *MockExecutionManager) TraderActor() interface{} { return nil }

func TestPositionService_GetAccountSnapshot(t *testing.T) {
	mockExec := new(MockExecutionManager)
	svc := NewService(mockExec)

	t.Run("Snapshot Success", func(t *testing.T) {

		snap, err := svc.GetAccountSnapshot(context.Background())
		assert.NoError(t, err)
		assert.NotNil(t, snap)

	})
}

func TestPositionService_ListPositions(t *testing.T) {
	mockExec := new(MockExecutionManager)
	svc := NewService(mockExec)

	t.Run("ListPositions Success", func(t *testing.T) {
		mockExec.On("ListOpenPositions", mock.Anything).Return([]exchange.Position{
			{Symbol: "BTC/USDT", Amount: 1.0},
		}, nil)

		positions, err := svc.ListPositions(context.Background())
		assert.NoError(t, err)
		assert.Len(t, positions, 1)
		assert.Equal(t, "BTC/USDT", positions[0].Symbol)
	})
}
