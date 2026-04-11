package auth

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInvalidPublicKey        = errors.New("invalid public key")
	ErrInvalidSessionID        = errors.New("invalid session id")
	ErrSessionNotFound         = errors.New("session not found")
	ErrSessionExpired          = errors.New("session expired")
	ErrSessionNotAuthenticated = errors.New("session not authenticated")
	ErrInvalidSignature        = errors.New("invalid signature")
	ErrInvalidDeviceID         = errors.New("invalid device id")
	ErrInvalidClientNonce      = errors.New("invalid client nonce")
	ErrSubscriptionNotFound    = errors.New("subscription not found")
)

type Session struct {
	SessionID        uuid.UUID
	UserPublicKey    []byte
	ClaimedPublicIP  string
	DeviceID         string
	ClientNonce      []byte
	ChallengePayload []byte
	IsAuthenticated  bool
	AuthenticatedAt  *time.Time
	ExpiresAt        time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type Subscription struct {
	SubscriptionID uuid.UUID
	SessionID      uuid.UUID
	UserPublicKey  []byte
	CreatedAt      time.Time
	UnsubscribedAt *time.Time
}

type Event struct {
	EventID          uuid.UUID
	UserPublicKey    []byte
	ReplyToRequestID *uuid.UUID
	EventType        string
	Payload          map[string]any
	AvailableAt      time.Time
	ExpiresAt        time.Time
	DeliveredAt      *time.Time
	CreatedAt        time.Time
}
