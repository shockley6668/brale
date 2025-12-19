package exchange

import "context"

type Exchange interface {
	Name() string

	OpenPosition(ctx context.Context, req OpenRequest) (*OpenResult, error)

	ClosePosition(ctx context.Context, req CloseRequest) error

	GetPosition(ctx context.Context, positionID string) (*Position, error)

	ListOpenPositions(ctx context.Context) ([]Position, error)

	GetBalance(ctx context.Context) (Balance, error)

	GetPrice(ctx context.Context, symbol string) (PriceQuote, error)
}

type WebhookProvider interface {
	HandleWebhook(ctx context.Context, payload map[string]any) error
}

type PriceSubscriber interface {
	SubscribePrice(ctx context.Context, symbol string, callback func(PriceQuote)) error

	UnsubscribePrice(ctx context.Context, symbol string) error
}
