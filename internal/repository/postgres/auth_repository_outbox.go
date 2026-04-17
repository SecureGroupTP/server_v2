package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/google/uuid"

	appoutbox "server_v2/internal/application/outbox"
	domainauth "server_v2/internal/domain/auth"
)

func (r *AuthRepository) appendOutbox(ctx context.Context, event domainauth.Event) error {
	deviceIDs, err := r.listTargetDeviceIDs(ctx, event.UserPublicKey)
	if err != nil {
		return err
	}
	if len(deviceIDs) == 0 {
		return nil
	}

	payload, err := cbor.Marshal(event.Payload)
	if err != nil {
		return fmt.Errorf("marshal outbox payload: %w", err)
	}

	segmentID := outboxSegmentID(event)
	for _, deviceID := range deviceIDs {
		outboxEventID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(event.EventID.String()+":"+deviceID))
		if _, err := r.dbtx(ctx).ExecContext(ctx, `
INSERT INTO outbox (
  event_id,
  device_id,
  segment_id,
  event_type,
  payload,
  status,
  send_after,
  inflight_until,
  attempts,
  created_at,
  expires_at,
  acked_at,
  last_sent_at,
  last_error
) VALUES ($1, $2, $3, $4, $5, $6, $7, NULL, 0, $8, $9, NULL, NULL, NULL)
ON CONFLICT (event_id) DO NOTHING
`, outboxEventID, deviceID, segmentID, event.EventType, payload, appoutbox.StatusPending, event.AvailableAt, event.CreatedAt, event.ExpiresAt); err != nil {
			return fmt.Errorf("insert outbox event: %w", err)
		}
		if _, err := r.dbtx(ctx).ExecContext(ctx, `
INSERT INTO outbox_segments (device_id, segment_id, head_event_id, inflight_event_id, updated_at)
VALUES ($1, $2, $3, NULL, $4)
ON CONFLICT (device_id, segment_id)
DO UPDATE SET
  head_event_id = COALESCE(outbox_segments.head_event_id, EXCLUDED.head_event_id),
  updated_at = EXCLUDED.updated_at
`, deviceID, segmentID, outboxEventID, event.CreatedAt); err != nil {
			return fmt.Errorf("upsert outbox segment: %w", err)
		}
	}
	return nil
}

func (r *AuthRepository) ClaimPending(ctx context.Context, now time.Time, batchSize int, ackTimeout time.Duration, maxAttempts int) ([]appoutbox.Event, error) {
	if batchSize <= 0 {
		return nil, nil
	}

	tx := currentTx(ctx)
	if tx == nil {
		return nil, fmt.Errorf("outbox claim requires transaction")
	}

	rows, err := tx.QueryContext(ctx, `
SELECT s.device_id, s.segment_id, s.head_event_id, o.status, o.attempts, o.created_at, o.expires_at
FROM outbox_segments s
JOIN outbox o ON o.event_id = s.head_event_id
WHERE s.head_event_id IS NOT NULL
  AND (
    o.expires_at <= $1
    OR (o.status = $2 AND o.send_after <= $1)
    OR (o.status = $3 AND o.inflight_until <= $1)
  )
ORDER BY o.created_at ASC
LIMIT $4
FOR UPDATE OF s SKIP LOCKED
`, now, appoutbox.StatusPending, appoutbox.StatusInFlight, batchSize)
	if err != nil {
		return nil, fmt.Errorf("select outbox segments: %w", err)
	}
	defer func() { _ = rows.Close() }()

	type candidate struct {
		deviceID  string
		segmentID string
		eventID   uuid.UUID
		status    int16
		attempts  int
		createdAt time.Time
		expiresAt time.Time
	}
	var candidates []candidate
	for rows.Next() {
		var item candidate
		if err := rows.Scan(&item.deviceID, &item.segmentID, &item.eventID, &item.status, &item.attempts, &item.createdAt, &item.expiresAt); err != nil {
			return nil, fmt.Errorf("scan outbox segment: %w", err)
		}
		candidates = append(candidates, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate outbox segments: %w", err)
	}

	events := make([]appoutbox.Event, 0, len(candidates))
	for _, item := range candidates {
		if item.expiresAt.Before(now) || item.expiresAt.Equal(now) || item.attempts+1 > maxAttempts {
			if err := r.dropSegmentTail(ctx, item.deviceID, item.segmentID, item.createdAt, now, "expired_or_attempts_exceeded"); err != nil {
				return nil, err
			}
			continue
		}

		inflightUntil := now.Add(ackTimeout)
		row := tx.QueryRowContext(ctx, `
UPDATE outbox
SET status = $2,
    attempts = attempts + 1,
    last_sent_at = $3,
    inflight_until = $4,
    send_after = $4,
    last_error = NULL
WHERE event_id = $1
RETURNING event_id, device_id, segment_id, event_type, payload, created_at, expires_at, inflight_until, last_sent_at, attempts
`, item.eventID, appoutbox.StatusInFlight, now, inflightUntil)

		event, err := scanOutboxEvent(row)
		if err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `
UPDATE outbox_segments
SET inflight_event_id = $3, updated_at = $4
WHERE device_id = $1 AND segment_id = $2
`, item.deviceID, item.segmentID, item.eventID, now); err != nil {
			return nil, fmt.Errorf("mark segment inflight: %w", err)
		}
		events = append(events, event)
	}
	return events, nil
}

func (r *AuthRepository) ListInflight(ctx context.Context, deviceID string, now time.Time, limit int) ([]appoutbox.Event, error) {
	rows, err := r.dbtx(ctx).QueryContext(ctx, `
SELECT event_id, device_id, segment_id, event_type, payload, created_at, expires_at, inflight_until, last_sent_at, attempts
FROM outbox
WHERE device_id = $1
  AND status = $2
  AND inflight_until > $3
ORDER BY created_at ASC
LIMIT $4
`, deviceID, appoutbox.StatusInFlight, now, limit)
	if err != nil {
		return nil, fmt.Errorf("query inflight outbox: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var events []appoutbox.Event
	for rows.Next() {
		event, err := scanOutboxRows(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate inflight outbox: %w", err)
	}
	return events, nil
}

func (r *AuthRepository) AcknowledgeOutbox(ctx context.Context, now time.Time, eventID uuid.UUID, deviceID string, segmentID string) error {
	tx := currentTx(ctx)
	if tx == nil {
		return fmt.Errorf("outbox ack requires transaction")
	}

	row := tx.QueryRowContext(ctx, `
SELECT segment_id, status
FROM outbox
WHERE event_id = $1 AND device_id = $2
`, eventID, deviceID)
	var storedSegmentID string
	var status int16
	if err := row.Scan(&storedSegmentID, &status); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return fmt.Errorf("select outbox event for ack: %w", err)
	}
	if segmentID != "" && segmentID != storedSegmentID {
		return nil
	}
	if status == appoutbox.StatusAcked {
		return nil
	}

	if _, err := tx.ExecContext(ctx, `
UPDATE outbox
SET status = $2, acked_at = $3
WHERE event_id = $1
`, eventID, appoutbox.StatusAcked, now); err != nil {
		return fmt.Errorf("ack outbox event: %w", err)
	}

	var headEventID uuid.UUID
	headErr := tx.QueryRowContext(ctx, `
SELECT COALESCE(head_event_id, '00000000-0000-0000-0000-000000000000'::uuid)
FROM outbox_segments
WHERE device_id = $1 AND segment_id = $2
`, deviceID, storedSegmentID).Scan(&headEventID)
	if headErr != nil && headErr != sql.ErrNoRows {
		return fmt.Errorf("select outbox head: %w", headErr)
	}
	if headErr == sql.ErrNoRows || headEventID != eventID {
		if _, err := tx.ExecContext(ctx, `
UPDATE outbox_segments
SET inflight_event_id = NULL, updated_at = $3
WHERE device_id = $1 AND segment_id = $2 AND inflight_event_id = $4
`, deviceID, storedSegmentID, now, eventID); err != nil {
			return fmt.Errorf("clear non-head inflight outbox event: %w", err)
		}
		return nil
	}

	var nextEventID *uuid.UUID
	row = tx.QueryRowContext(ctx, `
SELECT event_id
FROM outbox
WHERE device_id = $1
  AND segment_id = $2
  AND status IN ($3, $4)
  AND expires_at > $5
  AND event_id <> $6
ORDER BY created_at ASC
LIMIT 1
`, deviceID, storedSegmentID, appoutbox.StatusPending, appoutbox.StatusInFlight, now, eventID)
	var next uuid.UUID
	switch err := row.Scan(&next); err {
	case nil:
		nextEventID = &next
	case sql.ErrNoRows:
	default:
		return fmt.Errorf("select next outbox head: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
UPDATE outbox_segments
SET head_event_id = $3, inflight_event_id = NULL, updated_at = $4
WHERE device_id = $1 AND segment_id = $2
`, deviceID, storedSegmentID, nextEventID, now); err != nil {
		return fmt.Errorf("advance outbox head: %w", err)
	}
	return nil
}

func (r *AuthRepository) DropExpiredHeads(ctx context.Context, now time.Time, batchSize int) (int, error) {
	rows, err := r.dbtx(ctx).QueryContext(ctx, `
SELECT s.device_id, s.segment_id, o.created_at
FROM outbox_segments s
JOIN outbox o ON o.event_id = s.head_event_id
WHERE s.head_event_id IS NOT NULL AND o.expires_at <= $1
ORDER BY o.created_at ASC
LIMIT $2
`, now, batchSize)
	if err != nil {
		return 0, fmt.Errorf("query expired outbox heads: %w", err)
	}
	defer func() { _ = rows.Close() }()

	count := 0
	for rows.Next() {
		var deviceID string
		var segmentID string
		var createdAt time.Time
		if err := rows.Scan(&deviceID, &segmentID, &createdAt); err != nil {
			return count, fmt.Errorf("scan expired outbox head: %w", err)
		}
		if err := r.dropSegmentTail(ctx, deviceID, segmentID, createdAt, now, "expired_head"); err != nil {
			return count, err
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return count, fmt.Errorf("iterate expired outbox heads: %w", err)
	}
	return count, nil
}

func (r *AuthRepository) DeleteTerminal(ctx context.Context, status int16, olderThan time.Time, limit int) (int64, error) {
	result, err := r.dbtx(ctx).ExecContext(ctx, `
DELETE FROM outbox
WHERE event_id IN (
  SELECT event_id
  FROM outbox
  WHERE status = $1
    AND COALESCE(acked_at, expires_at) <= $2
  ORDER BY created_at ASC
  LIMIT $3
)
`, status, olderThan, limit)
	if err != nil {
		return 0, fmt.Errorf("delete terminal outbox events: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("terminal outbox rows affected: %w", err)
	}
	return rowsAffected, nil
}

func (r *AuthRepository) dropSegmentTail(ctx context.Context, deviceID string, segmentID string, fromCreatedAt time.Time, now time.Time, reason string) error {
	if _, err := r.dbtx(ctx).ExecContext(ctx, `
UPDATE outbox
SET status = $4, last_error = $5
WHERE device_id = $1
  AND segment_id = $2
  AND created_at >= $3
  AND status IN ($6, $7)
`, deviceID, segmentID, fromCreatedAt, appoutbox.StatusDropped, reason, appoutbox.StatusPending, appoutbox.StatusInFlight); err != nil {
		return fmt.Errorf("drop outbox tail: %w", err)
	}
	if _, err := r.dbtx(ctx).ExecContext(ctx, `
UPDATE outbox_segments
SET head_event_id = NULL, inflight_event_id = NULL, updated_at = $3
WHERE device_id = $1 AND segment_id = $2
`, deviceID, segmentID, now); err != nil {
		return fmt.Errorf("clear dropped outbox segment: %w", err)
	}
	return nil
}

func (r *AuthRepository) listTargetDeviceIDs(ctx context.Context, userPublicKey []byte) ([]string, error) {
	rows, err := r.dbtx(ctx).QueryContext(ctx, `
SELECT DISTINCT device_id
FROM (
  SELECT device_id
  FROM auth_sessions
  WHERE user_public_key = $1
    AND is_authenticated = TRUE
  UNION
  SELECT device_id
  FROM device_push_tokens
  WHERE user_public_key = $1
) devices
WHERE char_length(device_id) > 0
ORDER BY device_id ASC
`, userPublicKey)
	if err != nil {
		return nil, fmt.Errorf("query outbox devices: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var items []string
	for rows.Next() {
		var deviceID string
		if err := rows.Scan(&deviceID); err != nil {
			return nil, fmt.Errorf("scan outbox device: %w", err)
		}
		items = append(items, deviceID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate outbox devices: %w", err)
	}
	return items, nil
}

func outboxSegmentID(event domainauth.Event) string {
	if event.Payload != nil {
		for _, key := range []string{"roomId", "chatId", "segmentId"} {
			if value, ok := event.Payload[key].(string); ok && strings.TrimSpace(value) != "" {
				return value
			}
		}
	}
	return "global:" + event.EventType
}

func scanOutboxEvent(row interface {
	Scan(dest ...any) error
}) (appoutbox.Event, error) {
	var event appoutbox.Event
	var payload []byte
	if err := row.Scan(
		&event.EventID,
		&event.DeviceID,
		&event.SegmentID,
		&event.EventType,
		&payload,
		&event.CreatedAt,
		&event.ExpiresAt,
		&event.InflightUntil,
		&event.LastSentAt,
		&event.Attempts,
	); err != nil {
		return appoutbox.Event{}, fmt.Errorf("scan outbox event: %w", err)
	}
	if err := cbor.Unmarshal(payload, &event.Payload); err != nil {
		return appoutbox.Event{}, fmt.Errorf("unmarshal outbox payload: %w", err)
	}
	return event, nil
}

func scanOutboxRows(rows *sql.Rows) (appoutbox.Event, error) {
	return scanOutboxEvent(rows)
}
