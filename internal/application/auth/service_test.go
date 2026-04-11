package auth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"github.com/google/uuid"

	domainauth "server_v2/internal/domain/auth"
)

type fixedClock struct{ now time.Time }

func (f fixedClock) Now() time.Time { return f.now }

type noopTxManager struct{}

func (noopTxManager) WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(ctx)
}

type sequenceUUIDGenerator struct{ ids []uuid.UUID }

func (g *sequenceUUIDGenerator) New() uuid.UUID {
	id := g.ids[0]
	g.ids = g.ids[1:]
	return id
}

type fixedRandomReader struct{ data []byte }

func (r fixedRandomReader) Read(p []byte) (int, error) {
	copy(p, r.data)
	return len(p), nil
}

type sessionRepoMock struct {
	created         []domainauth.Session
	session         domainauth.Session
	markAuthSession domainauth.Session
	touchedKeys     [][]byte
}

func (m *sessionRepoMock) CreateSession(_ context.Context, session domainauth.Session) error {
	m.created = append(m.created, session)
	m.session = session
	return nil
}

func (m *sessionRepoMock) GetSession(_ context.Context, _ uuid.UUID) (domainauth.Session, error) {
	return m.session, nil
}

func (m *sessionRepoMock) MarkAuthenticated(_ context.Context, _ uuid.UUID, authenticatedAt time.Time) (domainauth.Session, error) {
	m.session.IsAuthenticated = true
	m.session.AuthenticatedAt = &authenticatedAt
	m.markAuthSession = m.session
	return m.session, nil
}

func (m *sessionRepoMock) TouchProfile(_ context.Context, publicKey []byte, _ time.Time) error {
	m.touchedKeys = append(m.touchedKeys, append([]byte(nil), publicKey...))
	return nil
}

func (m *sessionRepoMock) DeleteExpiredSessions(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}

type subscriptionRepoMock struct{ created []domainauth.Subscription }

func (m *subscriptionRepoMock) CreateSubscription(_ context.Context, subscription domainauth.Subscription) error {
	m.created = append(m.created, subscription)
	return nil
}

func (m *subscriptionRepoMock) DeactivateSubscription(_ context.Context, _ uuid.UUID, _ time.Time) error {
	return nil
}

type eventRepoMock struct {
	appended []domainauth.Event
	pending  []domainauth.Event
	marked   []uuid.UUID
}

func (m *eventRepoMock) Append(_ context.Context, event domainauth.Event) error {
	m.appended = append(m.appended, event)
	return nil
}

func (m *eventRepoMock) ListPending(_ context.Context, _ []byte, _ time.Time, _ int) ([]domainauth.Event, error) {
	return append([]domainauth.Event(nil), m.pending...), nil
}

func (m *eventRepoMock) MarkDelivered(_ context.Context, eventIDs []uuid.UUID, _ time.Time) error {
	m.marked = append(m.marked, eventIDs...)
	return nil
}

func (m *eventRepoMock) DeleteExpired(_ context.Context, _ time.Time) (int64, error) { return 0, nil }

func TestServiceAuthHandshakeFlow(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}

	now := time.Date(2026, 4, 11, 10, 0, 0, 0, time.UTC)
	sessionID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	eventID := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	sessions := &sessionRepoMock{}
	subscriptions := &subscriptionRepoMock{}
	events := &eventRepoMock{}
	service, err := NewService(
		Config{ChallengeTTL: 5 * time.Minute, EventRetention: 24 * time.Hour, EventBatchSize: 10},
		fixedClock{now: now},
		&sequenceUUIDGenerator{ids: []uuid.UUID{sessionID, eventID}},
		fixedRandomReader{data: []byte("01234567890123456789012345678901")},
		noopTxManager{},
		sessions,
		subscriptions,
		events,
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	challenge, err := service.RequestAuthChallenge(context.Background(), RequestAuthChallengeInput{
		UserPublicKey: publicKey,
		PublicIP:      "127.0.0.1",
		DeviceID:      "device-1",
		ClientNonce:   []byte("nonce"),
	})
	if err != nil {
		t.Fatalf("request challenge: %v", err)
	}
	if challenge.SessionID != sessionID {
		t.Fatalf("unexpected session id: %s", challenge.SessionID)
	}
	if len(sessions.created) != 1 {
		t.Fatalf("expected one created session, got %d", len(sessions.created))
	}

	signature := ed25519.Sign(privateKey, challenge.ChallengePayload)
	solveResult, err := service.SolveAuthChallenge(context.Background(), SolveAuthChallengeInput{
		SessionID: sessionID,
		Signature: signature,
	})
	if err != nil {
		t.Fatalf("solve challenge: %v", err)
	}
	if !solveResult.IsAuthenticated {
		t.Fatal("expected authenticated result")
	}
	if len(events.appended) != 1 {
		t.Fatalf("expected one appended event, got %d", len(events.appended))
	}
	if len(sessions.touchedKeys) != 1 {
		t.Fatalf("expected profile touch, got %d", len(sessions.touchedKeys))
	}
}

func TestServicePullEventsMarksDelivered(t *testing.T) {
	userPublicKey := make([]byte, ed25519.PublicKeySize)
	now := time.Date(2026, 4, 11, 11, 0, 0, 0, time.UTC)
	eventID := uuid.MustParse("33333333-3333-3333-3333-333333333333")

	events := &eventRepoMock{pending: []domainauth.Event{{EventID: eventID, UserPublicKey: userPublicKey, EventType: "test.event", Payload: map[string]any{"ok": true}, CreatedAt: now}}}
	service, err := NewService(
		Config{ChallengeTTL: time.Minute, EventRetention: time.Hour, EventBatchSize: 10},
		fixedClock{now: now},
		&sequenceUUIDGenerator{ids: []uuid.UUID{uuid.New()}},
		fixedRandomReader{data: make([]byte, challengeSize)},
		noopTxManager{},
		&sessionRepoMock{},
		&subscriptionRepoMock{},
		events,
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	pulled, err := service.PullEvents(context.Background(), PullEventsInput{UserPublicKey: userPublicKey})
	if err != nil {
		t.Fatalf("pull events: %v", err)
	}
	if len(pulled) != 1 {
		t.Fatalf("expected one event, got %d", len(pulled))
	}
	if len(events.marked) != 1 || events.marked[0] != eventID {
		t.Fatalf("expected event %s marked delivered, got %#v", eventID, events.marked)
	}
}
