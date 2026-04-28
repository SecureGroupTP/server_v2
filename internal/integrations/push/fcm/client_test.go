package fcm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	apppush "server_v2/internal/application/push"
)

func TestSendBuildsExpectedFCMRequest(t *testing.T) {
	t.Parallel()

	var capturedPath string
	var capturedAuth string
	var capturedBody map[string]any
	client := &Client{
		enabled:   true,
		endpoint:  "https://example.invalid",
		projectID: "demo-project",
		httpClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				capturedPath = req.URL.String()
				capturedAuth = req.Header.Get("Authorization")
				rawBody, err := io.ReadAll(req.Body)
				if err != nil {
					t.Fatalf("read body: %v", err)
				}
				if err := json.Unmarshal(rawBody, &capturedBody); err != nil {
					t.Fatalf("decode body: %v", err)
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{}`)),
					Header:     make(http.Header),
				}, nil
			}),
		},
	}

	err := client.Send(context.Background(), "token-1", apppush.Envelope{
		EventID:   "evt-1",
		EventType: "mlsMessageReceived",
		Kind:      apppush.KindMessage,
		DeviceID:  "device-1",
		SegmentID: "room-1",
		RoomID:    "room-1",
		CreatedAt: time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC),
		SafePayload: apppush.SafePayload{
			Title:        "Alice",
			Subtitle:     "1 new message",
			SenderID:     "peer-1",
			SenderName:   "Alice",
			MessageCount: 1,
		},
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if capturedPath != "https://example.invalid/v1/projects/demo-project/messages:send" {
		t.Fatalf("unexpected request path: %s", capturedPath)
	}
	if capturedAuth != "" {
		t.Fatalf("did not expect explicit auth header in stub client")
	}
	message, ok := capturedBody["message"].(map[string]any)
	if !ok {
		t.Fatalf("missing message body: %#v", capturedBody)
	}
	if message["token"] != "token-1" {
		t.Fatalf("unexpected token: %#v", message["token"])
	}
	data, ok := message["data"].(map[string]any)
	if !ok {
		t.Fatalf("missing data body: %#v", message["data"])
	}
	if data["eventType"] != "mlsMessageReceived" {
		t.Fatalf("unexpected event type: %#v", data["eventType"])
	}
	if data["deviceId"] != "device-1" {
		t.Fatalf("unexpected device id: %#v", data["deviceId"])
	}
	notification, ok := message["notification"].(map[string]any)
	if !ok {
		t.Fatalf("missing notification body: %#v", message["notification"])
	}
	if notification["title"] != "Alice" {
		t.Fatalf("unexpected notification title: %#v", notification["title"])
	}
	if notification["body"] != "1 new message" {
		t.Fatalf("unexpected notification body: %#v", notification["body"])
	}
	android, ok := message["android"].(map[string]any)
	if !ok {
		t.Fatalf("missing android body: %#v", message["android"])
	}
	androidNotification, ok := android["notification"].(map[string]any)
	if !ok {
		t.Fatalf("missing android notification body: %#v", android["notification"])
	}
	if androidNotification["channel_id"] != "sgtp_app_notifications" {
		t.Fatalf("unexpected android channel: %#v", androidNotification["channel_id"])
	}
	if androidNotification["icon"] != "ic_stat_sgtp_notification" {
		t.Fatalf("unexpected android icon: %#v", androidNotification["icon"])
	}
	if androidNotification["sound"] != "default" {
		t.Fatalf("unexpected android sound: %#v", androidNotification["sound"])
	}
	if androidNotification["default_sound"] != true {
		t.Fatalf("expected default android sound, got %#v", androidNotification["default_sound"])
	}
	if androidNotification["default_vibrate_timings"] != true {
		t.Fatalf("expected default android vibrate timings, got %#v", androidNotification["default_vibrate_timings"])
	}
	if androidNotification["notification_priority"] != "PRIORITY_HIGH" {
		t.Fatalf("unexpected android notification priority: %#v", androidNotification["notification_priority"])
	}
	if androidNotification["visibility"] != "PUBLIC" {
		t.Fatalf("unexpected android visibility: %#v", androidNotification["visibility"])
	}
}

func TestSendDisabledClientNoops(t *testing.T) {
	t.Parallel()

	client := &Client{}
	if err := client.Send(context.Background(), "token-1", apppush.Envelope{}); !errors.Is(err, apppush.ErrDisabled) {
		t.Fatalf("expected disabled client error, got %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
