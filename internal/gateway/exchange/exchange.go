package exchange

import "context"

// Exchange defines the common interface for trading exchange operations.
// Implementations include Freqtrade, and future CCXT or other exchange adapters.
type Exchange interface {
	// Name returns the exchange implementation name (e.g., "freqtrade", "ccxt-binance")
	Name() string

	// --- Position Management ---

	// OpenPosition opens a new trading position.
	// Returns the exchange-specific position/trade ID.
	OpenPosition(ctx context.Context, req OpenRequest) (*OpenResult, error)

	// ClosePosition closes an existing position (fully or partially).
	ClosePosition(ctx context.Context, req CloseRequest) error

	// GetPosition retrieves a specific position by its ID.
	GetPosition(ctx context.Context, positionID string) (*Position, error)

	// ListOpenPositions returns all currently open positions.
	ListOpenPositions(ctx context.Context) ([]Position, error)

	// --- Account ---

	// GetBalance retrieves the current account balance.
	GetBalance(ctx context.Context) (Balance, error)

	// --- Market Data ---

	// GetPrice retrieves the current price for a symbol.
	GetPrice(ctx context.Context, symbol string) (PriceQuote, error)
}

// WebhookProvider is an optional interface for exchanges that support webhooks.
// Implementations can choose to implement this for real-time updates.
type WebhookProvider interface {
	// HandleWebhook processes an incoming webhook message.
	// The message format is exchange-specific (parsed into map[string]any).
	HandleWebhook(ctx context.Context, payload map[string]any) error
}

// PriceSubscriber is an optional interface for exchanges that support price streaming.
type PriceSubscriber interface {
	// SubscribePrice subscribes to real-time price updates for a symbol.
	// The callback is called on each price update.
	SubscribePrice(ctx context.Context, symbol string, callback func(PriceQuote)) error

	// UnsubscribePrice stops receiving price updates for a symbol.
	UnsubscribePrice(ctx context.Context, symbol string) error
}
