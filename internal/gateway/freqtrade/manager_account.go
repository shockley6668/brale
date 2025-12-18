package freqtrade

import (
	"context"
	"fmt"

	"brale/internal/gateway/exchange"
)

// RefreshBalance fetches balance from client.
func (m *Manager) RefreshBalance(ctx context.Context) (exchange.Balance, error) {
	if m.client == nil {
		return exchange.Balance{}, fmt.Errorf("client not initialized")
	}
	bal, err := m.client.GetBalance(ctx)
	if err != nil {
		return exchange.Balance{}, err
	}
	m.balance = bal
	return bal, nil
}

// AccountBalance returns cached balance.
func (m *Manager) AccountBalance() exchange.Balance {
	return m.balance
}
