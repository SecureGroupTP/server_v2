package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	domainauth "server_v2/internal/domain/auth"
	"server_v2/internal/testutil/postgrestest"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	dsn := postgrestest.DSN(t)
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
	postgrestest.CleanupTables(t, store)
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

func TestAuthRepositorySubscriptionsCleanupAndTransactions(t *testing.T) {
	store := openTestStore(t)
	repo := NewAuthRepository(store.DB())
	txManager := NewTxManager(store.DB())
	ctx := context.Background()
	now := time.Date(2026, 4, 12, 15, 0, 0, 0, time.UTC)
	userPublicKey := []byte("99999999999999999999999999999999")
	sessionID := uuid.New()
	subscriptionID := uuid.New()
	expiredSessionID := uuid.New()
	expiredEventID := uuid.New()

	if err := repo.TouchProfile(ctx, userPublicKey, now); err != nil {
		t.Fatalf("touch profile: %v", err)
	}
	if err := repo.CreateSession(ctx, domainauth.Session{
		SessionID:        sessionID,
		UserPublicKey:    userPublicKey,
		DeviceID:         "device-1",
		ClientNonce:      []byte("nonce"),
		ChallengePayload: []byte("challenge-payload"),
		ExpiresAt:        now.Add(time.Hour),
		CreatedAt:        now,
		UpdatedAt:        now,
	}); err != nil {
		t.Fatalf("create live session: %v", err)
	}
	if err := repo.CreateSession(ctx, domainauth.Session{
		SessionID:        expiredSessionID,
		UserPublicKey:    userPublicKey,
		DeviceID:         "device-old",
		ClientNonce:      []byte("nonce"),
		ChallengePayload: []byte("challenge-payload"),
		ExpiresAt:        now.Add(-time.Minute),
		CreatedAt:        now.Add(-time.Hour),
		UpdatedAt:        now.Add(-time.Hour),
	}); err != nil {
		t.Fatalf("create expired session: %v", err)
	}
	if removed, err := repo.DeleteExpiredSessions(ctx, now); err != nil || removed != 1 {
		t.Fatalf("delete expired sessions removed=%d err=%v", removed, err)
	}

	if err := repo.CreateSubscription(ctx, domainauth.Subscription{SubscriptionID: subscriptionID, SessionID: sessionID, UserPublicKey: userPublicKey, CreatedAt: now}); err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	if err := repo.DeactivateSubscription(ctx, subscriptionID, now.Add(time.Minute)); err != nil {
		t.Fatalf("deactivate subscription: %v", err)
	}
	if err := repo.DeactivateSubscription(ctx, subscriptionID, now.Add(2*time.Minute)); err != domainauth.ErrSubscriptionNotFound {
		t.Fatalf("expected missing subscription after deactivation, got %v", err)
	}

	if err := repo.Append(ctx, domainauth.Event{EventID: expiredEventID, UserPublicKey: userPublicKey, EventType: "expired", Payload: map[string]any{"ok": true}, AvailableAt: now.Add(-time.Hour), ExpiresAt: now.Add(-time.Minute), CreatedAt: now.Add(-time.Hour)}); err != nil {
		t.Fatalf("append expired event: %v", err)
	}
	if removed, err := repo.DeleteExpired(ctx, now); err != nil || removed != 1 {
		t.Fatalf("delete expired events removed=%d err=%v", removed, err)
	}
	if err := repo.MarkDelivered(ctx, nil, now); err != nil {
		t.Fatalf("mark empty delivered should be no-op: %v", err)
	}

	committedID := uuid.New()
	if err := txManager.WithinTransaction(ctx, func(txCtx context.Context) error {
		if currentTx(txCtx) == nil {
			t.Fatal("expected transaction in context")
		}
		if currentDBTX(txCtx, store.DB()) == store.DB() {
			t.Fatal("expected dbtx to prefer transaction")
		}
		return repo.CreateSession(txCtx, domainauth.Session{
			SessionID:        committedID,
			UserPublicKey:    userPublicKey,
			DeviceID:         "device-tx",
			ClientNonce:      []byte("nonce"),
			ChallengePayload: []byte("challenge-payload"),
			ExpiresAt:        now.Add(time.Hour),
			CreatedAt:        now,
			UpdatedAt:        now,
		})
	}); err != nil {
		t.Fatalf("transaction commit: %v", err)
	}
	if _, err := repo.GetSession(ctx, committedID); err != nil {
		t.Fatalf("expected committed session: %v", err)
	}

	rolledBackID := uuid.New()
	rollbackErr := txManager.WithinTransaction(ctx, func(txCtx context.Context) error {
		return repo.CreateSession(txCtx, domainauth.Session{
			SessionID:        rolledBackID,
			UserPublicKey:    userPublicKey,
			DeviceID:         "device-rollback",
			ClientNonce:      []byte("nonce"),
			ChallengePayload: []byte("challenge-payload"),
			ExpiresAt:        now.Add(time.Hour),
			CreatedAt:        now,
			UpdatedAt:        now,
		})
	})
	if rollbackErr != nil {
		t.Fatalf("unexpected setup transaction error: %v", rollbackErr)
	}
	err := txManager.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := repo.CreateSession(txCtx, domainauth.Session{
			SessionID:        uuid.New(),
			UserPublicKey:    userPublicKey,
			DeviceID:         "device-fail",
			ClientNonce:      []byte("nonce"),
			ChallengePayload: []byte("challenge-payload"),
			ExpiresAt:        now.Add(time.Hour),
			CreatedAt:        now,
			UpdatedAt:        now,
		}); err != nil {
			return err
		}
		return domainauth.ErrInvalidSessionID
	})
	if err != domainauth.ErrInvalidSessionID {
		t.Fatalf("expected rollback error, got %v", err)
	}
}
