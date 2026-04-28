package fcm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	apppush "server_v2/internal/application/push"
	"server_v2/internal/config"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const (
	firebaseMessagingScope = "https://www.googleapis.com/auth/firebase.messaging"
	defaultEndpoint        = "https://fcm.googleapis.com"
	defaultChannelID       = "sgtp_app_notifications"
)

type Client struct {
	enabled    bool
	endpoint   string
	projectID  string
	httpClient *http.Client
}

func NewClient(ctx context.Context, cfg config.PushFCMConfiguration) (*Client, error) {
	if !cfg.Enabled {
		return &Client{}, nil
	}
	credentialsFile := strings.TrimSpace(cfg.CredentialsFile)
	if credentialsFile == "" {
		return nil, fmt.Errorf("push.fcm.credentials_file is required when enabled")
	}
	rawCredentials, err := os.ReadFile(credentialsFile)
	if err != nil {
		return nil, fmt.Errorf("read fcm credentials: %w", err)
	}
	credentials, err := google.CredentialsFromJSON(ctx, rawCredentials, firebaseMessagingScope)
	if err != nil {
		return nil, fmt.Errorf("parse fcm credentials: %w", err)
	}
	projectID := strings.TrimSpace(cfg.ProjectID)
	if projectID == "" {
		projectID = strings.TrimSpace(credentials.ProjectID)
	}
	if projectID == "" {
		return nil, fmt.Errorf("push.fcm.project_id is required when not present in credentials")
	}
	httpClient := oauth2.NewClient(ctx, credentials.TokenSource)
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	httpClient.Timeout = timeout
	endpoint := strings.TrimRight(strings.TrimSpace(cfg.Endpoint), "/")
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	return &Client{
		enabled:    true,
		endpoint:   endpoint,
		projectID:  projectID,
		httpClient: httpClient,
	}, nil
}

func (c *Client) Send(ctx context.Context, token string, envelope apppush.Envelope) error {
	if c == nil || !c.enabled {
		return apppush.ErrDisabled
	}
	body := map[string]any{
		"message": map[string]any{
			"token": token,
			"notification": map[string]any{
				"title": notificationTitle(envelope),
				"body":  notificationBody(envelope),
			},
			"data": apppush.EnvelopeData(envelope),
			"android": map[string]any{
				"priority": "high",
				"notification": map[string]any{
					"channel_id":              defaultChannelID,
					"icon":                    "ic_stat_sgtp_notification",
					"sound":                   "default",
					"default_sound":           true,
					"default_vibrate_timings": true,
					"notification_priority":   "PRIORITY_HIGH",
					"visibility":              "PUBLIC",
				},
			},
		},
	}
	rawBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal fcm request: %w", err)
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		fmt.Sprintf("%s/v1/projects/%s/messages:send", c.endpoint, c.projectID),
		bytes.NewReader(rawBody),
	)
	if err != nil {
		return fmt.Errorf("build fcm request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("send fcm request: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	}
	responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	return fmt.Errorf("fcm send failed: status=%d body=%s", response.StatusCode, strings.TrimSpace(string(responseBody)))
}

func notificationTitle(envelope apppush.Envelope) string {
	title := strings.TrimSpace(envelope.SafePayload.Title)
	if title != "" {
		return title
	}
	switch envelope.Kind {
	case apppush.KindMessage:
		return "New message"
	case apppush.KindFriendRequest:
		return "New activity"
	default:
		return "SGTP"
	}
}

func notificationBody(envelope apppush.Envelope) string {
	body := strings.TrimSpace(envelope.SafePayload.Subtitle)
	if body != "" {
		return body
	}
	switch envelope.Kind {
	case apppush.KindMessage:
		return "1 new message"
	case apppush.KindFriendRequest:
		return "Sent you a friend request"
	default:
		return "New notification"
	}
}
