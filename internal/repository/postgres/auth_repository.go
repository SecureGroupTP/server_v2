package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	appauth "server_v2/internal/application/auth"
	domainauth "server_v2/internal/domain/auth"
)

type AuthRepository struct {
	db *sql.DB
}

func NewAuthRepository(db *sql.DB) *AuthRepository {
	return &AuthRepository{db: db}
}

func (r *AuthRepository) dbtx(ctx context.Context) dbtx {
	return currentDBTX(ctx, r.db)
}

func (r *AuthRepository) CreateSession(ctx context.Context, session domainauth.Session) error {
	_, err := r.dbtx(ctx).ExecContext(ctx, `
INSERT INTO auth_sessions (
  session_id,
  user_public_key,
  claimed_public_ip,
  device_id,
  client_nonce,
  challenge_payload,
  is_authenticated,
  authenticated_at,
  expires_at,
  created_at,
  updated_at
) VALUES ($1, $2, NULLIF($3, '')::inet, $4, $5, $6, $7, $8, $9, $10, $11)
`,
		session.SessionID,
		session.UserPublicKey,
		session.ClaimedPublicIP,
		session.DeviceID,
		session.ClientNonce,
		session.ChallengePayload,
		session.IsAuthenticated,
		session.AuthenticatedAt,
		session.ExpiresAt,
		session.CreatedAt,
		session.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert auth session: %w", err)
	}
	return nil
}

func (r *AuthRepository) GetSession(ctx context.Context, sessionID uuid.UUID) (domainauth.Session, error) {
	row := r.dbtx(ctx).QueryRowContext(ctx, `
SELECT session_id, user_public_key, COALESCE(host(claimed_public_ip), ''), device_id, client_nonce,
       challenge_payload, is_authenticated, authenticated_at, expires_at, created_at, updated_at
FROM auth_sessions
WHERE session_id = $1
`, sessionID)

	var session domainauth.Session
	if err := row.Scan(
		&session.SessionID,
		&session.UserPublicKey,
		&session.ClaimedPublicIP,
		&session.DeviceID,
		&session.ClientNonce,
		&session.ChallengePayload,
		&session.IsAuthenticated,
		&session.AuthenticatedAt,
		&session.ExpiresAt,
		&session.CreatedAt,
		&session.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domainauth.Session{}, domainauth.ErrSessionNotFound
		}
		return domainauth.Session{}, fmt.Errorf("select auth session: %w", err)
	}
	return session, nil
}

func (r *AuthRepository) MarkAuthenticated(ctx context.Context, sessionID uuid.UUID, authenticatedAt time.Time) (domainauth.Session, error) {
	row := r.dbtx(ctx).QueryRowContext(ctx, `
UPDATE auth_sessions
SET is_authenticated = TRUE,
    authenticated_at = $2,
    updated_at = $2
WHERE session_id = $1
RETURNING session_id, user_public_key, COALESCE(host(claimed_public_ip), ''), device_id, client_nonce,
          challenge_payload, is_authenticated, authenticated_at, expires_at, created_at, updated_at
`, sessionID, authenticatedAt)

	var session domainauth.Session
	if err := row.Scan(
		&session.SessionID,
		&session.UserPublicKey,
		&session.ClaimedPublicIP,
		&session.DeviceID,
		&session.ClientNonce,
		&session.ChallengePayload,
		&session.IsAuthenticated,
		&session.AuthenticatedAt,
		&session.ExpiresAt,
		&session.CreatedAt,
		&session.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domainauth.Session{}, domainauth.ErrSessionNotFound
		}
		return domainauth.Session{}, fmt.Errorf("mark auth session authenticated: %w", err)
	}
	return session, nil
}

func (r *AuthRepository) TouchProfile(ctx context.Context, publicKey []byte, lastSeenAt time.Time) error {
	_, err := r.dbtx(ctx).ExecContext(ctx, `
INSERT INTO profiles (public_key, last_seen_at, created_at, updated_at)
VALUES ($1, $2, $2, $2)
ON CONFLICT (public_key)
DO UPDATE SET last_seen_at = EXCLUDED.last_seen_at, updated_at = EXCLUDED.updated_at
`, publicKey, lastSeenAt)
	if err != nil {
		return fmt.Errorf("touch profile: %w", err)
	}
	return nil
}

func (r *AuthRepository) DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error) {
	result, err := r.dbtx(ctx).ExecContext(ctx, `DELETE FROM auth_sessions WHERE expires_at <= $1`, now)
	if err != nil {
		return 0, fmt.Errorf("delete expired auth sessions: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("auth sessions rows affected: %w", err)
	}
	return rowsAffected, nil
}

func (r *AuthRepository) CreateSubscription(ctx context.Context, subscription domainauth.Subscription) error {
	_, err := r.dbtx(ctx).ExecContext(ctx, `
INSERT INTO event_subscriptions (subscription_id, session_id, user_public_key, created_at, unsubscribed_at)
VALUES ($1, $2, $3, $4, $5)
`, subscription.SubscriptionID, subscription.SessionID, subscription.UserPublicKey, subscription.CreatedAt, subscription.UnsubscribedAt)
	if err != nil {
		return fmt.Errorf("insert event subscription: %w", err)
	}
	return nil
}

func (r *AuthRepository) DeactivateSubscription(ctx context.Context, subscriptionID uuid.UUID, unsubscribedAt time.Time) error {
	result, err := r.dbtx(ctx).ExecContext(ctx, `
UPDATE event_subscriptions
SET unsubscribed_at = $2
WHERE subscription_id = $1 AND unsubscribed_at IS NULL
`, subscriptionID, unsubscribedAt)
	if err != nil {
		return fmt.Errorf("deactivate event subscription: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("event subscriptions rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return domainauth.ErrSubscriptionNotFound
	}
	return nil
}

func (r *AuthRepository) Append(ctx context.Context, event domainauth.Event) error {
	payload, err := json.Marshal(event.Payload)
	if err != nil {
		return fmt.Errorf("marshal event payload: %w", err)
	}

	_, err = r.dbtx(ctx).ExecContext(ctx, `
INSERT INTO user_events (
  event_id,
  user_public_key,
  reply_to_request_id,
  event_type,
  payload,
  available_at,
  expires_at,
  delivered_at,
  created_at
) VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8, $9)
`, event.EventID, event.UserPublicKey, event.ReplyToRequestID, event.EventType, string(payload), event.AvailableAt, event.ExpiresAt, event.DeliveredAt, event.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert user event: %w", err)
	}
	if err := r.appendOutbox(ctx, event); err != nil {
		return err
	}
	return nil
}

func (r *AuthRepository) ListPending(ctx context.Context, userPublicKey []byte, now time.Time, redeliverBefore time.Time, limit int) ([]domainauth.Event, error) {
	rows, err := r.dbtx(ctx).QueryContext(ctx, `
SELECT event_id, user_public_key, reply_to_request_id, event_type, payload, available_at, expires_at, delivered_at, created_at
FROM user_events
WHERE user_public_key = $1
  AND (delivered_at IS NULL OR delivered_at <= $3)
  AND available_at <= $2
  AND expires_at > $2
ORDER BY created_at ASC
LIMIT $4
`, userPublicKey, now, redeliverBefore, limit)
	if err != nil {
		return nil, fmt.Errorf("query pending user events: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	var events []domainauth.Event
	for rows.Next() {
		var event domainauth.Event
		var rawPayload []byte
		if err := rows.Scan(
			&event.EventID,
			&event.UserPublicKey,
			&event.ReplyToRequestID,
			&event.EventType,
			&rawPayload,
			&event.AvailableAt,
			&event.ExpiresAt,
			&event.DeliveredAt,
			&event.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan pending user event: %w", err)
		}
		if err := json.Unmarshal(rawPayload, &event.Payload); err != nil {
			return nil, fmt.Errorf("unmarshal pending user event payload: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending user events: %w", err)
	}
	return events, nil
}

func (r *AuthRepository) MarkDelivered(ctx context.Context, eventIDs []uuid.UUID, deliveredAt time.Time) error {
	if len(eventIDs) == 0 {
		return nil
	}
	_, err := r.dbtx(ctx).ExecContext(ctx, `
UPDATE user_events
SET delivered_at = $2
WHERE event_id = ANY($1)
`, eventIDs, deliveredAt)
	if err != nil {
		return fmt.Errorf("mark user events delivered: %w", err)
	}
	return nil
}

func (r *AuthRepository) Acknowledge(ctx context.Context, userPublicKey []byte, eventID uuid.UUID) error {
	_, err := r.dbtx(ctx).ExecContext(ctx, `
DELETE FROM user_events
WHERE event_id = $1 AND user_public_key = $2
`, eventID, userPublicKey)
	if err != nil {
		return fmt.Errorf("acknowledge user event: %w", err)
	}
	return nil
}

func (r *AuthRepository) DeleteExpired(ctx context.Context, now time.Time) (int64, error) {
	result, err := r.dbtx(ctx).ExecContext(ctx, `DELETE FROM user_events WHERE expires_at <= $1`, now)
	if err != nil {
		return 0, fmt.Errorf("delete expired user events: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("user events rows affected: %w", err)
	}
	return rowsAffected, nil
}

var (
	_ appauth.SessionRepository      = (*AuthRepository)(nil)
	_ appauth.SubscriptionRepository = (*AuthRepository)(nil)
	_ appauth.EventRepository        = (*AuthRepository)(nil)
)
