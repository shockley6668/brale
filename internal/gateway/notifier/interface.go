package notifier

type TextNotifier interface {
	SendText(text string) error
}
