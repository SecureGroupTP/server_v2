package push

import (
	"encoding/hex"
	"testing"
	"time"

	"github.com/google/uuid"

	appoutbox "server_v2/internal/application/outbox"
)

func TestMapOutboxEvent_MessageReceived(t *testing.T) {
	t.Parallel()

	sender := []byte{0xAA, 0xBB, 0xCC}
	eventID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	now := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)

	envelope, ok := MapOutboxEvent(
		appoutbox.Event{
			EventID:   eventID,
			DeviceID:  "device-1",
			SegmentID: "room-1",
			EventType: "mlsMessageReceived",
			Payload: map[string]any{
				"roomId":          "room-1",
				"messageId":       "msg-1",
				"senderPublicKey": sender,
			},
			CreatedAt: now,
		},
		func([]byte) string { return "Alice" },
	)
	if !ok {
		t.Fatalf("expected message event to be mapped")
	}
	if envelope.DeviceID != "device-1" {
		t.Fatalf("unexpected device id: %s", envelope.DeviceID)
	}
	if envelope.EventType != "mlsMessageReceived" {
		t.Fatalf("unexpected event type: %s", envelope.EventType)
	}
	if envelope.Kind != KindMessage {
		t.Fatalf("unexpected kind: %s", envelope.Kind)
	}
	if envelope.EventID != eventID.String() {
		t.Fatalf("unexpected event id: %s", envelope.EventID)
	}
	if envelope.RoomID != "room-1" {
		t.Fatalf("unexpected room id: %s", envelope.RoomID)
	}
	if envelope.SafePayload.SenderID != hex.EncodeToString(sender) {
		t.Fatalf("unexpected sender id: %s", envelope.SafePayload.SenderID)
	}
	if envelope.SafePayload.SenderName != "Alice" {
		t.Fatalf("unexpected sender name: %s", envelope.SafePayload.SenderName)
	}
	if envelope.SafePayload.MessageCount != 1 {
		t.Fatalf("unexpected message count: %d", envelope.SafePayload.MessageCount)
	}
}

func TestMapOutboxEvent_FriendRequest(t *testing.T) {
	t.Parallel()

	sender := []byte{0x10, 0x20, 0x30}
	envelope, ok := MapOutboxEvent(
		appoutbox.Event{
			EventID:   uuid.MustParse("22222222-2222-2222-2222-222222222222"),
			DeviceID:  "device-2",
			SegmentID: hex.EncodeToString(sender),
			EventType: "friend.requestReceived",
			Payload: map[string]any{
				"requestId":       "req-1",
				"senderPublicKey": sender,
			},
		},
		func([]byte) string { return "Bob" },
	)
	if !ok {
		t.Fatalf("expected friend request event to be mapped")
	}
	if envelope.Kind != KindFriendRequest {
		t.Fatalf("unexpected kind: %s", envelope.Kind)
	}
	if envelope.SafePayload.PeerID != hex.EncodeToString(sender) {
		t.Fatalf("unexpected peer id: %s", envelope.SafePayload.PeerID)
	}
	if envelope.SafePayload.DisplayName != "Bob" {
		t.Fatalf("unexpected display name: %s", envelope.SafePayload.DisplayName)
	}
}
