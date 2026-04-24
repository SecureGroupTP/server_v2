package outbox

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestDispatchOnce_NotifiesEventAndDeviceKey(t *testing.T) {
	t.Parallel()

	event := Event{
		EventID:   uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		DeviceID:  "device-1",
		SegmentID: "room-1",
		EventType: "mlsMessageReceived",
	}
	repo := &stubRepository{claimed: []Event{event}}
	notifier := &stubNotifier{}
	service, err := NewService(
		Config{},
		stubClock{now: time.Now()},
		stubTxManager{},
		repo,
		notifier,
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	dispatched, err := service.DispatchOnce(context.Background())
	if err != nil {
		t.Fatalf("dispatch once: %v", err)
	}
	if dispatched != 1 {
		t.Fatalf("unexpected dispatched count: %d", dispatched)
	}
	if len(notifier.events) != 1 {
		t.Fatalf("expected one event notification, got %d", len(notifier.events))
	}
	if len(notifier.keys) != 1 || notifier.keys[0] != "device-1" {
		t.Fatalf("unexpected key notifications: %#v", notifier.keys)
	}
}

type stubClock struct {
	now time.Time
}

func (s stubClock) Now() time.Time { return s.now }

type stubTxManager struct{}

func (stubTxManager) WithinTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

type stubRepository struct {
	claimed []Event
}

func (s *stubRepository) ClaimPending(context.Context, time.Time, int, time.Duration, int) ([]Event, error) {
	return s.claimed, nil
}

func (s *stubRepository) ListInflight(context.Context, string, time.Time, int) ([]Event, error) {
	return nil, nil
}

func (s *stubRepository) AcknowledgeOutbox(context.Context, time.Time, uuid.UUID, string, string) error {
	return nil
}

func (s *stubRepository) DropExpiredHeads(context.Context, time.Time, int) (int, error) {
	return 0, nil
}

func (s *stubRepository) DeleteTerminal(context.Context, int16, time.Time, int) (int64, error) {
	return 0, nil
}

type stubNotifier struct {
	keys   []string
	events []Event
}

func (s *stubNotifier) NotifyKey(key string) {
	s.keys = append(s.keys, key)
}

func (s *stubNotifier) NotifyOutboxEvent(event Event) {
	s.events = append(s.events, event)
}
