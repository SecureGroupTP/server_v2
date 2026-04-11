package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	domainauth "server_v2/internal/domain/auth"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN is not set")
	}

	store, err := Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	cleanupTables(t, store)
	return store
}

func cleanupTables(t *testing.T, store *Store) {
	t.Helper()
	_, err := store.DB().Exec(`TRUNCATE ban_statuses, user_events, event_subscriptions, auth_sessions, profiles RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("cleanup tables: %v", err)
	}
}

func TestAuthRepositoryRoundTrip(t *testing.T) {
	store := openTestStore(t)
	repo := NewAuthRepository(store.DB())
	now := time.Date(2026, 4, 11, 12, 0, 0, 0, time.UTC)
	sessionID := uuid.New()
	userPublicKey := []byte("12345678901234567890123456789012")

	session := domainauth.Session{
		SessionID:        sessionID,
		UserPublicKey:    userPublicKey,
		ClaimedPublicIP:  "127.0.0.1",
		DeviceID:         "device-1",
		ClientNonce:      []byte("nonce"),
		ChallengePayload: []byte("challenge-payload-000000000000000"),
		ExpiresAt:        now.Add(5 * time.Minute),
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := repo.CreateSession(context.Background(), session); err != nil {
		t.Fatalf("create session: %v", err)
	}

	loaded, err := repo.GetSession(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if loaded.DeviceID != session.DeviceID {
		t.Fatalf("unexpected device id: %s", loaded.DeviceID)
	}

	loaded, err = repo.MarkAuthenticated(context.Background(), sessionID, now)
	if err != nil {
		t.Fatalf("mark authenticated: %v", err)
	}
	if !loaded.IsAuthenticated {
		t.Fatal("expected authenticated session")
	}

	eventID := uuid.New()
	if err := repo.Append(context.Background(), domainauth.Event{
		EventID:       eventID,
		UserPublicKey: userPublicKey,
		EventType:     "auth.sessionAuthenticated",
		Payload:       map[string]any{"sessionId": sessionID.String()},
		AvailableAt:   now,
		ExpiresAt:     now.Add(time.Hour),
		CreatedAt:     now,
	}); err != nil {
		t.Fatalf("append event: %v", err)
	}

	events, err := repo.ListPending(context.Background(), userPublicKey, now.Add(time.Second), 10)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected one event, got %d", len(events))
	}

	if err := repo.MarkDelivered(context.Background(), []uuid.UUID{eventID}, now.Add(2*time.Second)); err != nil {
		t.Fatalf("mark delivered: %v", err)
	}
}
