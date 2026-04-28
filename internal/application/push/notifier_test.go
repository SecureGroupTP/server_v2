package push

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	appoutbox "server_v2/internal/application/outbox"
)

func TestNotifierQueuesAndSendsAndroidPush(t *testing.T) {
	t.Parallel()

	store := &stubStore{
		device: TargetDevice{
			DeviceID:  "device-1",
			Platform:  2,
			PushToken: "fcm-token",
			IsEnabled: true,
			Found:     true,
		},
		profileName: "Alice",
	}
	sender := &stubSender{}
	notifier := NewNotifier(store, sender, 4)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- notifier.Run(ctx)
	}()

	notifier.NotifyOutboxEvent(appoutbox.Event{
		EventID:   mustUUID("11111111-1111-1111-1111-111111111111"),
		DeviceID:  "device-1",
		SegmentID: "room-1",
		EventType: "mlsMessageReceived",
		Payload: map[string]any{
			"roomId":          "room-1",
			"messageId":       "msg-1",
			"senderPublicKey": []byte{0xAA},
		},
		CreatedAt: time.Now().UTC(),
	})

	deadline := time.Now().Add(2 * time.Second)
	for len(sender.calls) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(sender.calls) != 1 {
		t.Fatalf("expected one push send, got %d", len(sender.calls))
	}
	if sender.calls[0].token != "fcm-token" {
		t.Fatalf("unexpected token: %s", sender.calls[0].token)
	}
	cancel()
	if err := <-done; err != context.Canceled {
		t.Fatalf("unexpected run result: %v", err)
	}
}

func TestNotifierLogsPushSendFailures(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&out, &slog.HandlerOptions{Level: slog.LevelDebug}))
	store := &stubStore{
		device: TargetDevice{
			DeviceID:  "device-1",
			Platform:  2,
			PushToken: "fcm-token",
			IsEnabled: true,
			Found:     true,
		},
		profileName: "Alice",
	}
	sender := &stubSender{err: errors.New("fcm unavailable")}
	notifier := NewNotifierWithLogger(store, sender, 4, logger)

	notifier.process(context.Background(), appoutbox.Event{
		EventID:   mustUUID("22222222-2222-2222-2222-222222222222"),
		DeviceID:  "device-1",
		SegmentID: "global:friend.requestReceived",
		EventType: "friend.requestReceived",
		Payload: map[string]any{
			"requestId":       "request-1",
			"senderPublicKey": []byte{0xAA},
		},
		CreatedAt: time.Now().UTC(),
	})

	logged := out.String()
	if !strings.Contains(logged, "fcm push send failed") {
		t.Fatalf("expected fcm send failure log, got %q", logged)
	}
	if !strings.Contains(logged, "fcm unavailable") {
		t.Fatalf("expected fcm error in log, got %q", logged)
	}
	if strings.Contains(logged, "fcm-token") {
		t.Fatalf("push token leaked in log: %q", logged)
	}
}

func TestNotifierSkipsOutboxRetryAttempts(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&out, &slog.HandlerOptions{Level: slog.LevelDebug}))
	store := &stubStore{
		device: TargetDevice{
			DeviceID:  "device-1",
			Platform:  2,
			PushToken: "fcm-token",
			IsEnabled: true,
			Found:     true,
		},
		profileName: "Alice",
	}
	sender := &stubSender{}
	notifier := NewNotifierWithLogger(store, sender, 4, logger)

	notifier.process(context.Background(), appoutbox.Event{
		EventID:   mustUUID("33333333-3333-3333-3333-333333333333"),
		DeviceID:  "device-1",
		SegmentID: "global:friend.requestReceived",
		EventType: "friend.requestReceived",
		Payload: map[string]any{
			"requestId":       "request-1",
			"senderPublicKey": []byte{0xAA},
		},
		CreatedAt: time.Now().UTC(),
		Attempts:  2,
	})

	if len(sender.calls) != 0 {
		t.Fatalf("expected retry attempt to skip FCM send, got %d", len(sender.calls))
	}
	if !strings.Contains(out.String(), "fcm push skipped") {
		t.Fatalf("expected skip log, got %q", out.String())
	}
	if !strings.Contains(out.String(), "outbox_retry_attempt") {
		t.Fatalf("expected retry skip reason, got %q", out.String())
	}
}

type stubStore struct {
	device      TargetDevice
	profileName string
}

func (s *stubStore) LookupDevice(context.Context, string) (TargetDevice, error) {
	return s.device, nil
}

func (s *stubStore) LookupProfileName(context.Context, []byte) (string, error) {
	return s.profileName, nil
}

type stubSender struct {
	calls []sendCall
	err   error
}

type sendCall struct {
	token    string
	envelope Envelope
}

func (s *stubSender) Send(_ context.Context, token string, envelope Envelope) error {
	s.calls = append(s.calls, sendCall{token: token, envelope: envelope})
	return s.err
}

func mustUUID(value string) uuid.UUID {
	return uuid.MustParse(value)
}
