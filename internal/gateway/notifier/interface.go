package notifier

// TextNotifier defines a minimal text notification interface.
// It is intentionally small so different components can depend on it without
// importing concrete implementations (e.g. Telegram).
type TextNotifier interface {
	SendText(text string) error
}

