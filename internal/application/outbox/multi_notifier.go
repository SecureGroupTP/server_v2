package outbox

type MultiNotifier struct {
	notifiers []Notifier
}

func NewMultiNotifier(notifiers ...Notifier) *MultiNotifier {
	filtered := make([]Notifier, 0, len(notifiers))
	for _, notifier := range notifiers {
		if notifier != nil {
			filtered = append(filtered, notifier)
		}
	}
	return &MultiNotifier{notifiers: filtered}
}

func (n *MultiNotifier) NotifyKey(key string) {
	if n == nil {
		return
	}
	for _, notifier := range n.notifiers {
		notifier.NotifyKey(key)
	}
}

func (n *MultiNotifier) NotifyOutboxEvent(event Event) {
	if n == nil {
		return
	}
	for _, notifier := range n.notifiers {
		notifier.NotifyOutboxEvent(event)
	}
}
