package push

import (
	"encoding/hex"
	"fmt"
	"time"

	appoutbox "server_v2/internal/application/outbox"
)

func MapOutboxEvent(
	event appoutbox.Event,
	resolveProfileName func(publicKey []byte) string,
) (Envelope, bool) {
	switch event.EventType {
	case "mlsMessageReceived":
		return mapMessageEvent(event, resolveProfileName)
	case "friend.requestReceived":
		return mapFriendRequestEvent(event, resolveProfileName)
	default:
		return Envelope{}, false
	}
}

func mapMessageEvent(
	event appoutbox.Event,
	resolveProfileName func(publicKey []byte) string,
) (Envelope, bool) {
	roomID, _ := event.Payload["roomId"].(string)
	messageID, _ := event.Payload["messageId"].(string)
	senderPublicKey, _ := event.Payload["senderPublicKey"].([]byte)
	if roomID == "" || messageID == "" {
		return Envelope{}, false
	}
	senderID := hex.EncodeToString(senderPublicKey)
	senderName := "New message"
	if len(senderPublicKey) > 0 {
		if resolved := resolveProfileName(senderPublicKey); resolved != "" {
			senderName = resolved
		}
	}
	return Envelope{
		EventID:   event.EventID.String(),
		EventType: event.EventType,
		Kind:      KindMessage,
		DeviceID:  event.DeviceID,
		SegmentID: fallbackString(event.SegmentID, roomID),
		RoomID:    roomID,
		CreatedAt: fallbackTime(event.CreatedAt),
		SafePayload: SafePayload{
			Title:        senderName,
			Subtitle:     "1 new message",
			SenderID:     senderID,
			SenderName:   senderName,
			MessageCount: 1,
		},
	}, true
}

func mapFriendRequestEvent(
	event appoutbox.Event,
	resolveProfileName func(publicKey []byte) string,
) (Envelope, bool) {
	senderPublicKey, _ := event.Payload["senderPublicKey"].([]byte)
	if len(senderPublicKey) == 0 {
		return Envelope{}, false
	}
	peerID := hex.EncodeToString(senderPublicKey)
	displayName := "New activity"
	if resolved := resolveProfileName(senderPublicKey); resolved != "" {
		displayName = resolved
	}
	return Envelope{
		EventID:   event.EventID.String(),
		EventType: event.EventType,
		Kind:      KindFriendRequest,
		DeviceID:  event.DeviceID,
		SegmentID: fallbackString(event.SegmentID, peerID),
		CreatedAt: fallbackTime(event.CreatedAt),
		SafePayload: SafePayload{
			Title:       displayName,
			Subtitle:    "Sent you a friend request",
			PeerID:      peerID,
			DisplayName: displayName,
		},
	}, true
}

func EnvelopeData(envelope Envelope) map[string]string {
	data := map[string]string{
		"eventId":   envelope.EventID,
		"eventType": envelope.EventType,
		"kind":      envelope.Kind,
		"deviceId":  envelope.DeviceID,
		"segmentId": envelope.SegmentID,
		"createdAt": envelope.CreatedAt.UTC().Format(time.RFC3339Nano),
		"title":     envelope.SafePayload.Title,
		"subtitle":  envelope.SafePayload.Subtitle,
	}
	if envelope.RoomID != "" {
		data["roomId"] = envelope.RoomID
	}
	if envelope.SafePayload.SenderID != "" {
		data["senderId"] = envelope.SafePayload.SenderID
	}
	if envelope.SafePayload.SenderName != "" {
		data["senderName"] = envelope.SafePayload.SenderName
	}
	if envelope.SafePayload.PeerID != "" {
		data["peerId"] = envelope.SafePayload.PeerID
	}
	if envelope.SafePayload.DisplayName != "" {
		data["displayName"] = envelope.SafePayload.DisplayName
	}
	if envelope.SafePayload.MessageCount > 0 {
		data["messageCount"] = fmt.Sprintf("%d", envelope.SafePayload.MessageCount)
	}
	return data
}

func fallbackString(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func fallbackTime(value time.Time) time.Time {
	if value.IsZero() {
		return time.Now().UTC()
	}
	return value
}
