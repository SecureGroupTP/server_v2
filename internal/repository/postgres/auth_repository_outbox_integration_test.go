package postgres

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	appoutbox "server_v2/internal/application/outbox"
	domainauth "server_v2/internal/domain/auth"
)

type fixedClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fixedClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fixedClock) Set(now time.Time) {
	c.mu.Lock()
	c.now = now
	c.mu.Unlock()
}

func TestAuthRepositoryOutboxDispatchAndAcknowledgeOrder(t *testing.T) {
	store := openTestStore(t)
	repo := NewAuthRepository(store.DB())
	txManager := NewTxManager(store.DB())
	now := time.Date(2026, 4, 17, 18, 0, 0, 0, time.UTC)
	clock := &fixedClock{now: now}
	service, err := appoutbox.NewService(appoutbox.Config{
		PollInterval:      time.Second,
		BatchSizeSegments: 8,
		AckTimeout:        5 * time.Second,
		MaxAttempts:       5,
	}, clock, txManager, repo, nil)
	if err != nil {
		t.Fatalf("new outbox service: %v", err)
	}

	userPublicKey := []byte("12345678901234567890123456789012")
	seedAuthenticatedSession(t, repo, userPublicKey, "device-a", now)

	firstEventID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	secondEventID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	roomID := "room-1"
	for index, eventID := range []uuid.UUID{firstEventID, secondEventID} {
		createdAt := now.Add(time.Duration(index) * time.Second)
		if err := repo.Append(context.Background(), domainauth.Event{
			EventID:       eventID,
			UserPublicKey: userPublicKey,
			EventType:     "mlsMessageReceived",
			Payload:       map[string]any{"roomId": roomID, "messageId": eventID.String()},
			AvailableAt:   createdAt,
			ExpiresAt:     createdAt.Add(time.Hour),
			CreatedAt:     createdAt,
		}); err != nil {
			t.Fatalf("append outbox event %d: %v", index, err)
		}
	}

	if claimed, err := service.DispatchOnce(context.Background()); err != nil || claimed != 1 {
		t.Fatalf("dispatch once claimed=%d err=%v", claimed, err)
	}
	events, err := service.ListInflight(context.Background(), "device-a", 10)
	if err != nil {
		t.Fatalf("list inflight after first dispatch: %v", err)
	}
	if len(events) != 1 || events[0].Payload["messageId"] != firstEventID.String() {
		t.Fatalf("unexpected first inflight events: %#v", events)
	}

	if err := service.Acknowledge(context.Background(), events[0].EventID, "device-a", roomID); err != nil {
		t.Fatalf("ack first event: %v", err)
	}

	clock.Set(now.Add(2 * time.Second))
	if claimed, err := service.DispatchOnce(context.Background()); err != nil || claimed != 1 {
		t.Fatalf("dispatch second event claimed=%d err=%v", claimed, err)
	}
	events, err = service.ListInflight(context.Background(), "device-a", 10)
	if err != nil {
		t.Fatalf("list inflight after second dispatch: %v", err)
	}
	if len(events) != 1 || events[0].Payload["messageId"] != secondEventID.String() {
		t.Fatalf("unexpected second inflight events: %#v", events)
	}
}

func TestAuthRepositoryOutboxReclaimsTimedOutHead(t *testing.T) {
	store := openTestStore(t)
	repo := NewAuthRepository(store.DB())
	txManager := NewTxManager(store.DB())
	now := time.Date(2026, 4, 17, 19, 0, 0, 0, time.UTC)
	clock := &fixedClock{now: now}
	service, err := appoutbox.NewService(appoutbox.Config{
		BatchSizeSegments: 8,
		AckTimeout:        time.Second,
		MaxAttempts:       5,
	}, clock, txManager, repo, nil)
	if err != nil {
		t.Fatalf("new outbox service: %v", err)
	}

	userPublicKey := []byte("abcdefghabcdefghabcdefghabcdefgh")
	seedAuthenticatedSession(t, repo, userPublicKey, "device-retry", now)
	if err := repo.Append(context.Background(), domainauth.Event{
		EventID:       uuid.New(),
		UserPublicKey: userPublicKey,
		EventType:     "friend.requestReceived",
		Payload:       map[string]any{"requestId": uuid.New().String()},
		AvailableAt:   now,
		ExpiresAt:     now.Add(time.Hour),
		CreatedAt:     now,
	}); err != nil {
		t.Fatalf("append retry event: %v", err)
	}

	if _, err := service.DispatchOnce(context.Background()); err != nil {
		t.Fatalf("first dispatch: %v", err)
	}
	firstAttempt, err := service.ListInflight(context.Background(), "device-retry", 10)
	if err != nil || len(firstAttempt) != 1 || firstAttempt[0].Attempts != 1 {
		t.Fatalf("unexpected first attempt: events=%#v err=%v", firstAttempt, err)
	}

	clock.Set(now.Add(2 * time.Second))
	if claimed, err := service.DispatchOnce(context.Background()); err != nil || claimed != 1 {
		t.Fatalf("second dispatch claimed=%d err=%v", claimed, err)
	}
	secondAttempt, err := service.ListInflight(context.Background(), "device-retry", 10)
	if err != nil || len(secondAttempt) != 1 || secondAttempt[0].Attempts != 2 {
		t.Fatalf("unexpected second attempt: events=%#v err=%v", secondAttempt, err)
	}
}

func TestAuthRepositoryOutboxDropsExpiredHeadAndTail(t *testing.T) {
	store := openTestStore(t)
	repo := NewAuthRepository(store.DB())
	now := time.Date(2026, 4, 17, 20, 0, 0, 0, time.UTC)
	userPublicKey := []byte("ijklmnopijklmnopijklmnopijklmnop")
	seedAuthenticatedSession(t, repo, userPublicKey, "device-expired", now)
	roomID := "room-expired"

	for index := 0; index < 2; index++ {
		createdAt := now.Add(time.Duration(index) * time.Second)
		if err := repo.Append(context.Background(), domainauth.Event{
			EventID:       uuid.New(),
			UserPublicKey: userPublicKey,
			EventType:     "mlsMessageReceived",
			Payload:       map[string]any{"roomId": roomID, "messageId": uuid.New().String()},
			AvailableAt:   createdAt,
			ExpiresAt:     now.Add(-time.Second),
			CreatedAt:     createdAt,
		}); err != nil {
			t.Fatalf("append expired event %d: %v", index, err)
		}
	}

	if dropped, err := repo.DropExpiredHeads(context.Background(), now, 10); err != nil || dropped != 1 {
		t.Fatalf("drop expired heads dropped=%d err=%v", dropped, err)
	}

	var droppedCount int
	if err := store.DB().QueryRowContext(context.Background(), `
SELECT COUNT(*)
FROM outbox
WHERE device_id = $1 AND segment_id = $2 AND status = $3
`, "device-expired", roomID, appoutbox.StatusDropped).Scan(&droppedCount); err != nil {
		t.Fatalf("count dropped outbox rows: %v", err)
	}
	if droppedCount != 2 {
		t.Fatalf("expected tail drop of 2 rows, got %d", droppedCount)
	}
}

func TestAuthRepositoryOutboxClaimConcurrentSingleSegment(t *testing.T) {
	store := openTestStore(t)
	repo := NewAuthRepository(store.DB())
	txManager := NewTxManager(store.DB())
	now := time.Date(2026, 4, 17, 21, 0, 0, 0, time.UTC)
	clock := &fixedClock{now: now}
	service, err := appoutbox.NewService(appoutbox.Config{
		BatchSizeSegments: 8,
		AckTimeout:        5 * time.Second,
		MaxAttempts:       5,
	}, clock, txManager, repo, nil)
	if err != nil {
		t.Fatalf("new outbox service: %v", err)
	}

	userPublicKey := []byte("qrstuvwxqrstuvwxqrstuvwxqrstuvwx")
	seedAuthenticatedSession(t, repo, userPublicKey, "device-lock", now)
	if err := repo.Append(context.Background(), domainauth.Event{
		EventID:       uuid.New(),
		UserPublicKey: userPublicKey,
		EventType:     "friend.requestReceived",
		Payload:       map[string]any{"requestId": uuid.New().String()},
		AvailableAt:   now,
		ExpiresAt:     now.Add(time.Hour),
		CreatedAt:     now,
	}); err != nil {
		t.Fatalf("append concurrent event: %v", err)
	}

	var wg sync.WaitGroup
	results := make(chan int, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			claimed, err := service.DispatchOnce(context.Background())
			if err != nil {
				t.Errorf("dispatch once: %v", err)
				return
			}
			results <- claimed
		}()
	}
	wg.Wait()
	close(results)

	totalClaimed := 0
	for claimed := range results {
		totalClaimed += claimed
	}
	if totalClaimed != 1 {
		t.Fatalf("expected only one claim across concurrent workers, got %d", totalClaimed)
	}
}

func seedAuthenticatedSession(t *testing.T, repo *AuthRepository, userPublicKey []byte, deviceID string, now time.Time) {
	t.Helper()
	sessionID := uuid.New()
	if err := repo.CreateSession(context.Background(), domainauth.Session{
		SessionID:        sessionID,
		UserPublicKey:    userPublicKey,
		ClaimedPublicIP:  "127.0.0.1",
		DeviceID:         deviceID,
		ClientNonce:      []byte("nonce"),
		ChallengePayload: []byte("challenge-payload-000000000000000"),
		ExpiresAt:        now.Add(time.Hour),
		CreatedAt:        now,
		UpdatedAt:        now,
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := repo.MarkAuthenticated(context.Background(), sessionID, now); err != nil {
		t.Fatalf("mark authenticated: %v", err)
	}
}
