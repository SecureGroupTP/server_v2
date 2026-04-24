package push

import (
	"context"
	"errors"

	appoutbox "server_v2/internal/application/outbox"
)

type Store interface {
	LookupDevice(ctx context.Context, deviceID string) (TargetDevice, error)
	LookupProfileName(ctx context.Context, publicKey []byte) (string, error)
}

type Sender interface {
	Send(ctx context.Context, token string, envelope Envelope) error
}

type Notifier struct {
	store Store
	send  Sender
	queue chan appoutbox.Event
}

func NewNotifier(store Store, send Sender, queueSize int) *Notifier {
	if queueSize <= 0 {
		queueSize = 64
	}
	return &Notifier{
		store: store,
		send:  send,
		queue: make(chan appoutbox.Event, queueSize),
	}
}

func (n *Notifier) NotifyOutboxEvent(event appoutbox.Event) {
	if n == nil {
		return
	}
	select {
	case n.queue <- event:
	default:
	}
}

func (n *Notifier) NotifyKey(string) {}

func (n *Notifier) Run(ctx context.Context) error {
	if n == nil || n.store == nil || n.send == nil {
		<-ctx.Done()
		return ctx.Err()
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event := <-n.queue:
			n.process(ctx, event)
		}
	}
}

func (n *Notifier) process(ctx context.Context, event appoutbox.Event) {
	device, err := n.store.LookupDevice(ctx, event.DeviceID)
	if err != nil || !device.Found || !device.IsEnabled || device.PushToken == "" {
		return
	}
	if device.Platform != 2 {
		return
	}
	envelope, ok := MapOutboxEvent(event, func(publicKey []byte) string {
		name, err := n.store.LookupProfileName(ctx, publicKey)
		if err != nil {
			return ""
		}
		return name
	})
	if !ok {
		return
	}
	_ = n.send.Send(ctx, device.PushToken, envelope)
}

var ErrDisabled = errors.New("push sender disabled")
