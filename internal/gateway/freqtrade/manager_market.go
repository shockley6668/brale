package freqtrade

import (
	"encoding/json"
	"strings"
	"time"

	"brale/internal/gateway/exchange"
	"brale/internal/logger"
	"brale/internal/strategy"
	"brale/internal/trader"
)

func (m *Manager) PublishPrice(symbol string, quote exchange.PriceQuote) {
	if m.trader == nil {
		return
	}
	payload, _ := json.Marshal(trader.PriceUpdatePayload{
		Symbol: symbol,
		Quote: strategy.MarketQuote{
			Last: quote.Last,
			High: quote.High,
			Low:  quote.Low,
		},
	})
	if err := m.trader.Send(trader.EventEnvelope{
		ID:        managerEventID("", "price"),
		Type:      trader.EvtPriceUpdate,
		Payload:   payload,
		CreatedAt: time.Now(),
		Symbol:    strings.ToUpper(strings.TrimSpace(symbol)),
	}); err != nil {
		logger.Warnf("freqtrade manager: PublishPrice send failed: %v", err)
	}
}
