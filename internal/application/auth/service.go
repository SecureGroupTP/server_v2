package auth

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/google/uuid"

	domainauth "server_v2/internal/domain/auth"
	domaintx "server_v2/internal/domain/tx"
)

const challengeSize = 32

type Clock interface {
	Now() time.Time
}

type UUIDGenerator interface {
	New() uuid.UUID
}

type RandomReader interface {
	Read(p []byte) (int, error)
}

type SessionRepository interface {
	CreateSession(ctx context.Context, session domainauth.Session) error
	GetSession(ctx context.Context, sessionID uuid.UUID) (domainauth.Session, error)
	MarkAuthenticated(ctx context.Context, sessionID uuid.UUID, authenticatedAt time.Time) (domainauth.Session, error)
	TouchProfile(ctx context.Context, publicKey []byte, lastSeenAt time.Time) error
	DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error)
}

type SubscriptionRepository interface {
	CreateSubscription(ctx context.Context, subscription domainauth.Subscription) error
	DeactivateSubscription(ctx context.Context, subscriptionID uuid.UUID, unsubscribedAt time.Time) error
}

type EventRepository interface {
	Append(ctx context.Context, event domainauth.Event) error
	ListPending(ctx context.Context, userPublicKey []byte, now time.Time, limit int) ([]domainauth.Event, error)
	MarkDelivered(ctx context.Context, eventIDs []uuid.UUID, deliveredAt time.Time) error
	DeleteExpired(ctx context.Context, now time.Time) (int64, error)
}

type Config struct {
	ChallengeTTL   time.Duration
	EventRetention time.Duration
	EventBatchSize int
}

type RequestAuthChallengeInput struct {
	UserPublicKey []byte
	PublicIP      string
	DeviceID      string
	ClientNonce   []byte
}

type RequestAuthChallengeOutput struct {
	SessionID        uuid.UUID
	ChallengePayload []byte
	ExpiresAt        time.Time
}

type SolveAuthChallengeInput struct {
	SessionID uuid.UUID
	Signature []byte
}

type SolveAuthChallengeOutput struct {
	IsAuthenticated bool
	UserPublicKey   []byte
	ServerTime      time.Time
}

type SubscribeToEventsInput struct {
	SessionID uuid.UUID
}

type SubscribeToEventsOutput struct {
	SubscriptionID uuid.UUID
	SubscribedAt   time.Time
}

type UnsubscribeFromEventsInput struct {
	SubscriptionID uuid.UUID
}

type UnsubscribeFromEventsOutput struct {
	UnsubscribedAt time.Time
}

type PullEventsInput struct {
	UserPublicKey []byte
	Limit         int
}

type Service struct {
	cfg           Config
	clock         Clock
	uuidGenerator UUIDGenerator
	randomReader  RandomReader
	txManager     domaintx.Manager
	sessions      SessionRepository
	subscriptions SubscriptionRepository
	events        EventRepository
}

func NewService(
	cfg Config,
	clock Clock,
	uuidGenerator UUIDGenerator,
	randomReader RandomReader,
	txManager domaintx.Manager,
	sessions SessionRepository,
	subscriptions SubscriptionRepository,
	events EventRepository,
) (*Service, error) {
	if cfg.ChallengeTTL <= 0 {
		return nil, fmt.Errorf("challenge ttl must be > 0")
	}
	if cfg.EventRetention <= 0 {
		return nil, fmt.Errorf("event retention must be > 0")
	}
	if cfg.EventBatchSize <= 0 {
		cfg.EventBatchSize = 100
	}
	if clock == nil || uuidGenerator == nil || randomReader == nil || txManager == nil || sessions == nil || subscriptions == nil || events == nil {
		return nil, fmt.Errorf("all dependencies are required")
	}

	return &Service{
		cfg:           cfg,
		clock:         clock,
		uuidGenerator: uuidGenerator,
		randomReader:  randomReader,
		txManager:     txManager,
		sessions:      sessions,
		subscriptions: subscriptions,
		events:        events,
	}, nil
}

func (s *Service) RequestAuthChallenge(ctx context.Context, input RequestAuthChallengeInput) (RequestAuthChallengeOutput, error) {
	if len(input.UserPublicKey) != ed25519.PublicKeySize {
		return RequestAuthChallengeOutput{}, domainauth.ErrInvalidPublicKey
	}
	if strings.TrimSpace(input.DeviceID) == "" {
		return RequestAuthChallengeOutput{}, domainauth.ErrInvalidDeviceID
	}
	if len(input.ClientNonce) == 0 {
		return RequestAuthChallengeOutput{}, domainauth.ErrInvalidClientNonce
	}
	if input.PublicIP != "" {
		if parsedIP := net.ParseIP(input.PublicIP); parsedIP == nil {
			return RequestAuthChallengeOutput{}, fmt.Errorf("invalid public ip")
		}
	}

	now := s.clock.Now()
	challengePayload := make([]byte, challengeSize)
	if _, err := s.randomReader.Read(challengePayload); err != nil {
		return RequestAuthChallengeOutput{}, fmt.Errorf("generate challenge: %w", err)
	}

	session := domainauth.Session{
		SessionID:        s.uuidGenerator.New(),
		UserPublicKey:    append([]byte(nil), input.UserPublicKey...),
		ClaimedPublicIP:  input.PublicIP,
		DeviceID:         input.DeviceID,
		ClientNonce:      append([]byte(nil), input.ClientNonce...),
		ChallengePayload: challengePayload,
		ExpiresAt:        now.Add(s.cfg.ChallengeTTL),
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if err := s.sessions.CreateSession(ctx, session); err != nil {
		return RequestAuthChallengeOutput{}, err
	}

	return RequestAuthChallengeOutput{
		SessionID:        session.SessionID,
		ChallengePayload: append([]byte(nil), session.ChallengePayload...),
		ExpiresAt:        session.ExpiresAt,
	}, nil
}

func (s *Service) SolveAuthChallenge(ctx context.Context, input SolveAuthChallengeInput) (SolveAuthChallengeOutput, error) {
	if input.SessionID == uuid.Nil {
		return SolveAuthChallengeOutput{}, domainauth.ErrInvalidSessionID
	}

	session, err := s.sessions.GetSession(ctx, input.SessionID)
	if err != nil {
		return SolveAuthChallengeOutput{}, err
	}

	now := s.clock.Now()
	if now.After(session.ExpiresAt) {
		return SolveAuthChallengeOutput{}, domainauth.ErrSessionExpired
	}
	if !ed25519.Verify(session.UserPublicKey, session.ChallengePayload, input.Signature) {
		return SolveAuthChallengeOutput{}, domainauth.ErrInvalidSignature
	}

	if err := s.txManager.WithinTransaction(ctx, func(txCtx context.Context) error {
		var markErr error
		session, markErr = s.sessions.MarkAuthenticated(txCtx, session.SessionID, now)
		if markErr != nil {
			return markErr
		}
		if err := s.sessions.TouchProfile(txCtx, session.UserPublicKey, now); err != nil {
			return err
		}

		return s.events.Append(txCtx, domainauth.Event{
			EventID:       s.uuidGenerator.New(),
			UserPublicKey: append([]byte(nil), session.UserPublicKey...),
			EventType:     "auth.sessionAuthenticated",
			Payload: map[string]any{
				"sessionId":       session.SessionID.String(),
				"authenticatedAt": now.UTC().Format(time.RFC3339Nano),
			},
			AvailableAt: now,
			ExpiresAt:   now.Add(s.cfg.EventRetention),
			CreatedAt:   now,
		})
	}); err != nil {
		return SolveAuthChallengeOutput{}, err
	}

	return SolveAuthChallengeOutput{
		IsAuthenticated: true,
		UserPublicKey:   append([]byte(nil), session.UserPublicKey...),
		ServerTime:      now,
	}, nil
}

func (s *Service) LookupSession(ctx context.Context, sessionID uuid.UUID) (domainauth.Session, error) {
	return s.sessions.GetSession(ctx, sessionID)
}

func (s *Service) SubscribeToEvents(ctx context.Context, input SubscribeToEventsInput) (SubscribeToEventsOutput, error) {
	session, err := s.sessions.GetSession(ctx, input.SessionID)
	if err != nil {
		return SubscribeToEventsOutput{}, err
	}
	if !session.IsAuthenticated {
		return SubscribeToEventsOutput{}, domainauth.ErrSessionNotAuthenticated
	}

	now := s.clock.Now()
	subscription := domainauth.Subscription{
		SubscriptionID: s.uuidGenerator.New(),
		SessionID:      session.SessionID,
		UserPublicKey:  append([]byte(nil), session.UserPublicKey...),
		CreatedAt:      now,
	}
	if err := s.txManager.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := s.subscriptions.CreateSubscription(txCtx, subscription); err != nil {
			return err
		}

		return s.events.Append(txCtx, domainauth.Event{
			EventID:       s.uuidGenerator.New(),
			UserPublicKey: append([]byte(nil), session.UserPublicKey...),
			EventType:     "auth.eventsSubscribed",
			Payload: map[string]any{
				"subscriptionId": subscription.SubscriptionID.String(),
				"subscribedAt":   now.UTC().Format(time.RFC3339Nano),
			},
			AvailableAt: now,
			ExpiresAt:   now.Add(s.cfg.EventRetention),
			CreatedAt:   now,
		})
	}); err != nil {
		return SubscribeToEventsOutput{}, err
	}

	return SubscribeToEventsOutput{
		SubscriptionID: subscription.SubscriptionID,
		SubscribedAt:   now,
	}, nil
}

func (s *Service) UnsubscribeFromEvents(ctx context.Context, input UnsubscribeFromEventsInput) (UnsubscribeFromEventsOutput, error) {
	now := s.clock.Now()
	if err := s.subscriptions.DeactivateSubscription(ctx, input.SubscriptionID, now); err != nil {
		return UnsubscribeFromEventsOutput{}, err
	}
	return UnsubscribeFromEventsOutput{UnsubscribedAt: now}, nil
}

func (s *Service) PullEvents(ctx context.Context, input PullEventsInput) ([]domainauth.Event, error) {
	if len(input.UserPublicKey) != ed25519.PublicKeySize {
		return nil, domainauth.ErrInvalidPublicKey
	}

	limit := input.Limit
	if limit <= 0 || limit > s.cfg.EventBatchSize {
		limit = s.cfg.EventBatchSize
	}

	now := s.clock.Now()
	events, err := s.events.ListPending(ctx, input.UserPublicKey, now, limit)
	if err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, nil
	}

	eventIDs := make([]uuid.UUID, 0, len(events))
	for _, event := range events {
		eventIDs = append(eventIDs, event.EventID)
	}
	if err := s.txManager.WithinTransaction(ctx, func(txCtx context.Context) error {
		return s.events.MarkDelivered(txCtx, eventIDs, now)
	}); err != nil {
		return nil, err
	}

	return events, nil
}
