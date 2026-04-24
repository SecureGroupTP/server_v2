package push

import (
	"context"
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
}

type sendCall struct {
	token    string
	envelope Envelope
}

func (s *stubSender) Send(_ context.Context, token string, envelope Envelope) error {
	s.calls = append(s.calls, sendCall{token: token, envelope: envelope})
	return nil
}

func mustUUID(value string) uuid.UUID {
	return uuid.MustParse(value)
}
