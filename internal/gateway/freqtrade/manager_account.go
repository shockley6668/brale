package freqtrade

import (
	"context"
	"fmt"

	"brale/internal/gateway/exchange"
)

func (m *Manager) RefreshBalance(ctx context.Context) (exchange.Balance, error) {
	if m == nil || m.client == nil {
		return exchange.Balance{}, fmt.Errorf("client not initialized")
	}
	bal, err := m.client.GetBalance(ctx)
	if err != nil {
		return exchange.Balance{}, err
	}
	m.balance = bal
	return bal, nil
}

func (m *Manager) AccountBalance() exchange.Balance {
	if m == nil {
		return exchange.Balance{}
	}
	return m.balance
}
