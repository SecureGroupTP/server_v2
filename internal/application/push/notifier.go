package push

import (
	"context"
	"errors"
	"log/slog"

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
	store  Store
	send   Sender
	queue  chan appoutbox.Event
	logger *slog.Logger
}

func NewNotifier(store Store, send Sender, queueSize int) *Notifier {
	return NewNotifierWithLogger(store, send, queueSize, nil)
}

func NewNotifierWithLogger(store Store, send Sender, queueSize int, logger *slog.Logger) *Notifier {
	if queueSize <= 0 {
		queueSize = 64
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Notifier{
		store:  store,
		send:   send,
		queue:  make(chan appoutbox.Event, queueSize),
		logger: logger,
	}
}

func (n *Notifier) NotifyOutboxEvent(event appoutbox.Event) {
	if n == nil {
		return
	}
	select {
	case n.queue <- event:
	default:
		n.logger.Warn(
			"fcm push queue full; dropping outbox event",
			"event_id", event.EventID.String(),
			"event_type", event.EventType,
			"device_id", event.DeviceID,
			"segment_id", event.SegmentID,
		)
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
	envelope, ok := MapOutboxEvent(event, func(publicKey []byte) string {
		name, err := n.store.LookupProfileName(ctx, publicKey)
		if err != nil {
			n.logger.Debug(
				"fcm push profile lookup failed",
				"event_id", event.EventID.String(),
				"event_type", event.EventType,
				"device_id", event.DeviceID,
				"error", err,
			)
			return ""
		}
		return name
	})
	if !ok {
		n.logger.Debug(
			"fcm push skipped",
			"reason", "unsupported_or_invalid_event",
			"event_id", event.EventID.String(),
			"event_type", event.EventType,
			"device_id", event.DeviceID,
			"segment_id", event.SegmentID,
		)
		return
	}

	device, err := n.store.LookupDevice(ctx, event.DeviceID)
	if err != nil {
		n.logger.Warn(
			"fcm push device lookup failed",
			"event_id", event.EventID.String(),
			"event_type", event.EventType,
			"kind", envelope.Kind,
			"device_id", event.DeviceID,
			"segment_id", event.SegmentID,
			"error", err,
		)
		return
	}
	if !device.Found {
		n.logSkipped(event, envelope, device, "device_not_registered_for_push")
		return
	}
	if !device.IsEnabled {
		n.logSkipped(event, envelope, device, "device_push_disabled")
		return
	}
	if device.PushToken == "" {
		n.logSkipped(event, envelope, device, "device_push_token_empty")
		return
	}
	if device.Platform != 2 {
		n.logSkipped(event, envelope, device, "platform_not_android")
		return
	}
	if err := n.send.Send(ctx, device.PushToken, envelope); err != nil {
		if errors.Is(err, ErrDisabled) {
			n.logSkipped(event, envelope, device, "fcm_disabled")
			return
		}
		n.logger.Warn(
			"fcm push send failed",
			"event_id", event.EventID.String(),
			"event_type", event.EventType,
			"kind", envelope.Kind,
			"device_id", event.DeviceID,
			"segment_id", event.SegmentID,
			"platform", device.Platform,
			"error", err,
		)
		return
	}
	n.logger.Info(
		"fcm push sent",
		"event_id", event.EventID.String(),
		"event_type", event.EventType,
		"kind", envelope.Kind,
		"device_id", event.DeviceID,
		"segment_id", event.SegmentID,
		"platform", device.Platform,
	)
}

func (n *Notifier) logSkipped(event appoutbox.Event, envelope Envelope, device TargetDevice, reason string) {
	n.logger.Info(
		"fcm push skipped",
		"reason", reason,
		"event_id", event.EventID.String(),
		"event_type", event.EventType,
		"kind", envelope.Kind,
		"device_id", event.DeviceID,
		"segment_id", event.SegmentID,
		"platform", device.Platform,
		"device_found", device.Found,
		"push_enabled", device.IsEnabled,
		"has_push_token", device.PushToken != "",
	)
}

var ErrDisabled = errors.New("push sender disabled")
