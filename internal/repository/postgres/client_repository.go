package postgres

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	clientapi "server_v2/internal/application/clientapi"
)

type ClientRepository struct {
	db *sql.DB
}

func decodeAnyBytes(value any) ([]byte, error) {
	switch v := value.(type) {
	case nil:
		return nil, nil
	case []byte:
		return append([]byte(nil), v...), nil
	case string:
		if v == "" {
			return nil, nil
		}
		out, err := base64.StdEncoding.DecodeString(v)
		if err != nil {
			return nil, err
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unexpected bytes value type %T", value)
	}
}

func NewClientRepository(db *sql.DB) *ClientRepository {
	return &ClientRepository{db: db}
}

func (r *ClientRepository) dbtx(ctx context.Context) dbtx {
	return currentDBTX(ctx, r.db)
}

func (r *ClientRepository) GetProfile(ctx context.Context, publicKey []byte) (clientapi.ProfileRecord, error) {
	row := r.dbtx(ctx).QueryRowContext(ctx, `
SELECT public_key, COALESCE(username, ''), COALESCE(display_name, ''), COALESCE(bio, ''), COALESCE(avatar_hash, ''),
       COALESCE(avatar_bytes, ''::bytea), COALESCE(avatar_content_type, ''), last_seen_at, updated_at, deleted_at
FROM profiles
WHERE public_key = $1
`, publicKey)
	var record clientapi.ProfileRecord
	if err := row.Scan(&record.PublicKey, &record.Username, &record.DisplayName, &record.Bio, &record.AvatarHash, &record.AvatarBytes, &record.ContentType, &record.LastSeenAt, &record.UpdatedAt, &record.DeletedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return clientapi.ProfileRecord{}, clientapi.ErrNotFound
		}
		return clientapi.ProfileRecord{}, fmt.Errorf("get profile: %w", err)
	}
	return record, nil
}

func (r *ClientRepository) GetActiveBanStatus(ctx context.Context, publicKey []byte, now time.Time) (clientapi.BanStatusRecord, bool, error) {
	row := r.dbtx(ctx).QueryRowContext(ctx, `
SELECT is_banned, COALESCE(reason, ''), banned_at, expires_at
FROM ban_statuses
WHERE public_key = $1
  AND is_banned = TRUE
  AND (expires_at IS NULL OR expires_at > $2)
`, publicKey, now)
	var record clientapi.BanStatusRecord
	if err := row.Scan(&record.IsBanned, &record.Reason, &record.BannedAt, &record.ExpiresAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return clientapi.BanStatusRecord{}, false, nil
		}
		return clientapi.BanStatusRecord{}, false, fmt.Errorf("get active ban status: %w", err)
	}
	return record, true, nil
}

func (r *ClientRepository) UpdateProfile(ctx context.Context, publicKey []byte, username string, displayName string, avatarHash string, bio string, updatedAt time.Time) error {
	_, err := r.dbtx(ctx).ExecContext(ctx, `
INSERT INTO profiles (public_key, username, display_name, bio, avatar_hash, last_seen_at, created_at, updated_at)
VALUES ($1, NULLIF($2, ''), NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), $6, $6, $6)
ON CONFLICT (public_key)
DO UPDATE SET username = NULLIF($2, ''), display_name = NULLIF($3, ''), bio = NULLIF($4, ''), avatar_hash = NULLIF($5, ''), updated_at = $6, deleted_at = NULL
`, publicKey, username, displayName, bio, avatarHash, updatedAt)
	if err != nil {
		return fmt.Errorf("update profile: %w", err)
	}
	return nil
}

func (r *ClientRepository) SearchProfiles(ctx context.Context, query string, limit int, offset int) ([]clientapi.ProfileRecord, error) {
	rows, err := r.dbtx(ctx).QueryContext(ctx, `
SELECT public_key, COALESCE(username, ''), COALESCE(display_name, ''), COALESCE(bio, ''), COALESCE(avatar_hash, ''),
       COALESCE(avatar_bytes, ''::bytea), COALESCE(avatar_content_type, ''), last_seen_at, updated_at, deleted_at
FROM profiles
WHERE deleted_at IS NULL AND (
  POSITION(LOWER($1) IN LOWER(COALESCE(username, ''))) > 0 OR
  POSITION(LOWER($1) IN LOWER(COALESCE(display_name, ''))) > 0
)
ORDER BY updated_at DESC
LIMIT $2 OFFSET $3
`, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("search profiles: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanProfiles(rows)
}

func (r *ClientRepository) DeleteAccount(ctx context.Context, publicKey []byte, deletedAt time.Time) error {
	queries := []struct {
		q    string
		args []any
	}{
		{`UPDATE profiles SET deleted_at = $2, updated_at = $2 WHERE public_key = $1`, []any{publicKey, deletedAt}},
		{`DELETE FROM device_push_tokens WHERE user_public_key = $1`, []any{publicKey}},
		{`DELETE FROM key_packages WHERE user_public_key = $1`, []any{publicKey}},
		{`DELETE FROM friends WHERE user_public_key = $1 OR friend_public_key = $1`, []any{publicKey}},
		{`DELETE FROM friend_requests WHERE sender_public_key = $1 OR receiver_public_key = $1`, []any{publicKey}},
		{`UPDATE chat_members SET left_at = $2 WHERE user_public_key = $1 AND left_at IS NULL`, []any{publicKey, deletedAt}},
		{`DELETE FROM chat_member_permissions WHERE user_public_key = $1`, []any{publicKey}},
		{`DELETE FROM chat_invitations WHERE inviter_public_key = $1 OR invitee_public_key = $1`, []any{publicKey}},
	}
	for _, item := range queries {
		if _, err := r.dbtx(ctx).ExecContext(ctx, item.q, item.args...); err != nil {
			return err
		}
	}
	return nil
}

func (r *ClientRepository) GetProfileAvatar(ctx context.Context, publicKey []byte) (clientapi.AvatarRecord, error) {
	row := r.dbtx(ctx).QueryRowContext(ctx, `SELECT COALESCE(avatar_bytes, ''::bytea), COALESCE(avatar_content_type, '') FROM profiles WHERE public_key = $1`, publicKey)
	var avatar clientapi.AvatarRecord
	if err := row.Scan(&avatar.Bytes, &avatar.ContentType); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return clientapi.AvatarRecord{}, clientapi.ErrNotFound
		}
		return clientapi.AvatarRecord{}, err
	}
	return avatar, nil
}

func (r *ClientRepository) ListDevices(ctx context.Context, userPublicKey []byte) ([]clientapi.DeviceRecord, error) {
	rows, err := r.dbtx(ctx).QueryContext(ctx, `SELECT id, session_id, user_public_key, device_id, platform, push_token, is_enabled, updated_at FROM device_push_tokens WHERE user_public_key = $1 ORDER BY updated_at DESC`, userPublicKey)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var items []clientapi.DeviceRecord
	for rows.Next() {
		var item clientapi.DeviceRecord
		if err := rows.Scan(&item.ID, &item.SessionID, &item.UserPublicKey, &item.DeviceID, &item.Platform, &item.PushToken, &item.IsEnabled, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *ClientRepository) UpsertDevice(ctx context.Context, device clientapi.DeviceRecord) (clientapi.DeviceRecord, error) {
	row := r.dbtx(ctx).QueryRowContext(ctx, `
INSERT INTO device_push_tokens (id, session_id, user_public_key, device_id, platform, push_token, is_enabled, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (user_public_key, device_id)
DO UPDATE SET session_id = EXCLUDED.session_id, platform = EXCLUDED.platform, push_token = EXCLUDED.push_token, is_enabled = EXCLUDED.is_enabled, updated_at = EXCLUDED.updated_at
RETURNING id, session_id, user_public_key, device_id, platform, push_token, is_enabled, updated_at
`, device.ID, device.SessionID, device.UserPublicKey, device.DeviceID, device.Platform, device.PushToken, device.IsEnabled, device.UpdatedAt)
	var out clientapi.DeviceRecord
	if err := row.Scan(&out.ID, &out.SessionID, &out.UserPublicKey, &out.DeviceID, &out.Platform, &out.PushToken, &out.IsEnabled, &out.UpdatedAt); err != nil {
		return clientapi.DeviceRecord{}, err
	}
	return out, nil
}

func (r *ClientRepository) RemoveDevice(ctx context.Context, userPublicKey []byte, deviceID uuid.UUID, removedAt time.Time) error {
	deviceIdentifier, err := r.lookupDeviceIdentifier(ctx, userPublicKey, deviceID)
	if err != nil {
		return err
	}
	if _, err := r.dbtx(ctx).ExecContext(ctx, `DELETE FROM device_push_tokens WHERE id = $1 AND user_public_key = $2`, deviceID, userPublicKey); err != nil {
		return err
	}
	return r.DeleteKeyPackagesByUserDevice(ctx, userPublicKey, deviceIdentifier)
}

func (r *ClientRepository) lookupDeviceIdentifier(ctx context.Context, userPublicKey []byte, deviceID uuid.UUID) (string, error) {
	row := r.dbtx(ctx).QueryRowContext(ctx, `SELECT device_id FROM device_push_tokens WHERE id = $1 AND user_public_key = $2`, deviceID, userPublicKey)
	var deviceIdentifier string
	if err := row.Scan(&deviceIdentifier); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", clientapi.ErrNotFound
		}
		return "", err
	}
	return deviceIdentifier, nil
}

func (r *ClientRepository) InsertKeyPackages(ctx context.Context, keyPackages []clientapi.KeyPackageRecord) (int, error) {
	for _, item := range keyPackages {
		if _, err := r.dbtx(ctx).ExecContext(ctx, `INSERT INTO key_packages (id, user_public_key, device_id, key_package_bytes, is_last_resort, created_at, expires_at) VALUES ($1,$2,$3,$4,$5,$6,$7)`, item.ID, item.UserPublicKey, item.DeviceID, item.KeyPackageBytes, item.IsLastResort, item.CreatedAt, item.ExpiresAt); err != nil {
			return 0, err
		}
	}
	return len(keyPackages), nil
}

func (r *ClientRepository) FetchKeyPackages(ctx context.Context, userPublicKeys [][]byte, now time.Time) ([]clientapi.KeyPackageRecord, error) {
	// Fetch a single "best" key package per requested user.
	//
	// Rationale:
	// - Direct rooms store only one Welcome per (room_id, target_user_public_key),
	//   so inviting the wrong device_id (stale / old install) makes the stored
	//   welcome unusable for the currently active device.
	// - In practice, most flows assume one active MLS device per account.
	//
	// Selection heuristic (per user):
	// 1) Prefer device_id with an active authenticated auth session.
	// 2) Else prefer device_id with an enabled push token.
	// 3) Else fall back to the device_id that uploaded the most recent key package.
	// Within the chosen device_id, prefer non-last-resort packages and the newest.
	rows, err := r.dbtx(ctx).QueryContext(ctx, `
SELECT DISTINCT ON (kp.user_public_key)
  kp.id, kp.user_public_key, kp.device_id, kp.key_package_bytes, kp.is_last_resort, kp.created_at, kp.expires_at
FROM key_packages kp
LEFT JOIN LATERAL (
  SELECT s.device_id
  FROM auth_sessions s
  WHERE s.user_public_key = kp.user_public_key
    AND s.is_authenticated = TRUE
    AND s.expires_at > $2
  ORDER BY s.updated_at DESC
  LIMIT 1
) active_session ON TRUE
LEFT JOIN LATERAL (
  SELECT t.device_id
  FROM device_push_tokens t
  WHERE t.user_public_key = kp.user_public_key
    AND t.is_enabled = TRUE
  ORDER BY t.updated_at DESC
  LIMIT 1
) active_push ON TRUE
WHERE kp.user_public_key = ANY($1)
  AND kp.expires_at > $2
ORDER BY
  kp.user_public_key,
  CASE
    WHEN active_session.device_id IS NOT NULL AND kp.device_id = active_session.device_id THEN 0
    WHEN active_push.device_id IS NOT NULL AND kp.device_id = active_push.device_id THEN 1
    ELSE 2
  END,
  kp.is_last_resort ASC,
  kp.created_at DESC,
  kp.device_id ASC
`, pqByteaArray(userPublicKeys), now)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var items []clientapi.KeyPackageRecord
	for rows.Next() {
		var item clientapi.KeyPackageRecord
		if err := rows.Scan(&item.ID, &item.UserPublicKey, &item.DeviceID, &item.KeyPackageBytes, &item.IsLastResort, &item.CreatedAt, &item.ExpiresAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *ClientRepository) DeleteKeyPackagesByUserDevice(ctx context.Context, userPublicKey []byte, deviceID string) error {
	_, err := r.dbtx(ctx).ExecContext(ctx, `DELETE FROM key_packages WHERE user_public_key = $1 AND device_id = $2`, userPublicKey, deviceID)
	return err
}

func (r *ClientRepository) UpsertRoomGroupInfo(ctx context.Context, userPublicKey []byte, groupInfo clientapi.ChatRoomGroupInfoRecord) error {
	if _, err := r.ensureMember(ctx, groupInfo.RoomID, userPublicKey); err != nil {
		return err
	}
	_, err := r.dbtx(ctx).ExecContext(ctx, `
INSERT INTO chat_room_group_infos (room_id, uploader_public_key, group_info_bytes, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (room_id)
DO UPDATE SET uploader_public_key = EXCLUDED.uploader_public_key, group_info_bytes = EXCLUDED.group_info_bytes, updated_at = EXCLUDED.updated_at
`, groupInfo.RoomID, groupInfo.UploaderPublicKey, groupInfo.GroupInfoBytes, groupInfo.CreatedAt, groupInfo.UpdatedAt)
	return err
}

func (r *ClientRepository) GetRoomGroupInfo(ctx context.Context, roomID uuid.UUID) (clientapi.ChatRoomGroupInfoRecord, error) {
	row := r.dbtx(ctx).QueryRowContext(ctx, `
SELECT room_id, uploader_public_key, group_info_bytes, created_at, updated_at
FROM chat_room_group_infos
WHERE room_id = $1
`, roomID)
	var out clientapi.ChatRoomGroupInfoRecord
	if err := row.Scan(&out.RoomID, &out.UploaderPublicKey, &out.GroupInfoBytes, &out.CreatedAt, &out.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return clientapi.ChatRoomGroupInfoRecord{}, clientapi.ErrNotFound
		}
		return clientapi.ChatRoomGroupInfoRecord{}, err
	}
	return out, nil
}

func (r *ClientRepository) FindDirectRoomIDByUsers(ctx context.Context, leftUserPublicKey []byte, rightUserPublicKey []byte) (uuid.UUID, bool, error) {
	left, right := orderedPublicKeyPair(leftUserPublicKey, rightUserPublicKey)
	row := r.dbtx(ctx).QueryRowContext(ctx, `
SELECT d.room_id
FROM direct_rooms d
JOIN chat_rooms r ON r.room_id = d.room_id
WHERE d.left_user_public_key = $1
  AND d.right_user_public_key = $2
  AND r.deleted_at IS NULL
`, left, right)
	var roomID uuid.UUID
	if err := row.Scan(&roomID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return uuid.Nil, false, nil
		}
		return uuid.Nil, false, err
	}
	return roomID, true, nil
}

func (r *ClientRepository) CreateDirectRoom(ctx context.Context, room clientapi.ChatRoomRecord, left clientapi.ChatMemberRecord, right clientapi.ChatMemberRecord, direct clientapi.DirectRoomRecord) error {
	if err := r.CreateRoom(ctx, room, left); err != nil {
		return err
	}
	if _, err := r.dbtx(ctx).ExecContext(ctx, `
INSERT INTO direct_rooms (room_id, left_user_public_key, right_user_public_key, created_at)
VALUES ($1, $2, $3, $4)
`, direct.RoomID, direct.LeftUserPublicKey, direct.RightUserPublicKey, direct.CreatedAt); err != nil {
		return err
	}
	_, err := r.dbtx(ctx).ExecContext(ctx, `
INSERT INTO chat_members (room_id, user_public_key, role, notification_level, joined_at)
VALUES ($1, $2, $3, $4, $5)
`, right.RoomID, right.UserPublicKey, right.Role, right.NotificationLevel, right.JoinedAt)
	return err
}

func (r *ClientRepository) IsDirectRoom(ctx context.Context, roomID uuid.UUID) (bool, error) {
	row := r.dbtx(ctx).QueryRowContext(ctx, `
SELECT EXISTS (
  SELECT 1
  FROM direct_rooms d
  JOIN chat_rooms r ON r.room_id = d.room_id
  WHERE d.room_id = $1
    AND r.deleted_at IS NULL
)
`, roomID)
	var exists bool
	if err := row.Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func (r *ClientRepository) UpsertRoomWelcome(ctx context.Context, userPublicKey []byte, welcome clientapi.ChatRoomWelcomeRecord) error {
	if _, err := r.ensureMember(ctx, welcome.RoomID, userPublicKey); err != nil {
		return err
	}
	if _, err := r.ensureMember(ctx, welcome.RoomID, welcome.TargetUserPublicKey); err != nil {
		return err
	}

	direct, err := r.IsDirectRoom(ctx, welcome.RoomID)
	if err != nil {
		return err
	}
	if !direct {
		return clientapi.ErrForbidden
	}

	var activeMembers int64
	if err := r.dbtx(ctx).QueryRowContext(ctx, `SELECT COUNT(*) FROM chat_members WHERE room_id = $1 AND left_at IS NULL`, welcome.RoomID).Scan(&activeMembers); err != nil {
		return err
	}
	if activeMembers != 2 {
		return clientapi.ErrForbidden
	}

	_, err = r.dbtx(ctx).ExecContext(ctx, `
INSERT INTO chat_room_welcomes (
  room_id,
  target_user_public_key,
  sender_public_key,
  welcome_bytes,
  created_at,
  updated_at
) VALUES ($1, $2, $3, $4, COALESCE($5, NOW()), COALESCE($6, NOW()))
ON CONFLICT (room_id, target_user_public_key)
DO UPDATE SET
  sender_public_key = EXCLUDED.sender_public_key,
  welcome_bytes = EXCLUDED.welcome_bytes,
  updated_at = EXCLUDED.updated_at
`, welcome.RoomID, welcome.TargetUserPublicKey, welcome.SenderPublicKey, welcome.WelcomeBytes, welcome.CreatedAt, welcome.UpdatedAt)
	return err
}

func (r *ClientRepository) GetRoomWelcome(ctx context.Context, roomID uuid.UUID, targetUserPublicKey []byte) (clientapi.ChatRoomWelcomeRecord, error) {
	if _, err := r.ensureMember(ctx, roomID, targetUserPublicKey); err != nil {
		return clientapi.ChatRoomWelcomeRecord{}, err
	}

	direct, err := r.IsDirectRoom(ctx, roomID)
	if err != nil {
		return clientapi.ChatRoomWelcomeRecord{}, err
	}
	if !direct {
		return clientapi.ChatRoomWelcomeRecord{}, clientapi.ErrForbidden
	}

	var activeMembers int64
	if err := r.dbtx(ctx).QueryRowContext(ctx, `SELECT COUNT(*) FROM chat_members WHERE room_id = $1 AND left_at IS NULL`, roomID).Scan(&activeMembers); err != nil {
		return clientapi.ChatRoomWelcomeRecord{}, err
	}
	if activeMembers != 2 {
		return clientapi.ChatRoomWelcomeRecord{}, clientapi.ErrForbidden
	}

	// Primary: durable welcome storage.
	{
		row := r.dbtx(ctx).QueryRowContext(ctx, `
SELECT sender_public_key, welcome_bytes, created_at, updated_at
FROM chat_room_welcomes
WHERE room_id = $1 AND target_user_public_key = $2
`, roomID, targetUserPublicKey)
		var senderPublicKey []byte
		var welcomeBytes []byte
		var createdAt time.Time
		var updatedAt time.Time
		switch err := row.Scan(&senderPublicKey, &welcomeBytes, &createdAt, &updatedAt); err {
		case nil:
			return clientapi.ChatRoomWelcomeRecord{
				RoomID:              roomID,
				TargetUserPublicKey: append([]byte(nil), targetUserPublicKey...),
				SenderPublicKey:     senderPublicKey,
				WelcomeBytes:        welcomeBytes,
				CreatedAt:           createdAt,
				UpdatedAt:           updatedAt,
			}, nil
		case sql.ErrNoRows:
			// fall through to legacy outbox-backed storage below
		default:
			return clientapi.ChatRoomWelcomeRecord{}, err
		}
	}

	// Legacy fallback: some older builds stored welcomes only as user events
	// ("outbox"). Keep reading them for compatibility and backfill into the
	// durable table when found.
	row := r.dbtx(ctx).QueryRowContext(ctx, `
SELECT payload, created_at
FROM user_events
WHERE user_public_key = $1
  AND event_type = 'mlsWelcomeReceived'
  AND payload->>'roomId' = $2
  AND available_at <= NOW()
  AND expires_at > NOW()
ORDER BY created_at DESC
LIMIT 1
`, targetUserPublicKey, roomID.String())
	var rawPayload []byte
	var createdAt time.Time
	if err := row.Scan(&rawPayload, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return clientapi.ChatRoomWelcomeRecord{}, clientapi.ErrNotFound
		}
		return clientapi.ChatRoomWelcomeRecord{}, err
	}
	var decoded map[string]any
	if err := json.Unmarshal(rawPayload, &decoded); err != nil {
		return clientapi.ChatRoomWelcomeRecord{}, err
	}
	welcomeBytes, err := decodeAnyBytes(decoded["welcomeBytes"])
	if err != nil {
		return clientapi.ChatRoomWelcomeRecord{}, err
	}
	senderPublicKey, _ := decodeAnyBytes(decoded["senderPublicKey"])

	// Best-effort backfill into durable storage to prevent future `not_found`
	// if user event retention/ACK semantics differ across server builds.
	_, _ = r.dbtx(ctx).ExecContext(ctx, `
INSERT INTO chat_room_welcomes (
  room_id,
  target_user_public_key,
  sender_public_key,
  welcome_bytes,
  created_at,
  updated_at
) VALUES ($1, $2, $3, $4, $5, $5)
ON CONFLICT (room_id, target_user_public_key)
DO UPDATE SET
  sender_public_key = EXCLUDED.sender_public_key,
  welcome_bytes = EXCLUDED.welcome_bytes,
  updated_at = EXCLUDED.updated_at
`, roomID, targetUserPublicKey, senderPublicKey, welcomeBytes, createdAt)

	return clientapi.ChatRoomWelcomeRecord{
		RoomID:              roomID,
		TargetUserPublicKey: append([]byte(nil), targetUserPublicKey...),
		SenderPublicKey:     senderPublicKey,
		WelcomeBytes:        welcomeBytes,
		CreatedAt:           createdAt,
		UpdatedAt:           createdAt,
	}, nil
}

func (r *ClientRepository) AreFriends(ctx context.Context, leftUserPublicKey []byte, rightUserPublicKey []byte) (bool, error) {
	row := r.dbtx(ctx).QueryRowContext(ctx, `
SELECT EXISTS (
  SELECT 1
  FROM friends left_friend
  JOIN friends right_friend
    ON right_friend.user_public_key = left_friend.friend_public_key
   AND right_friend.friend_public_key = left_friend.user_public_key
  WHERE left_friend.user_public_key = $1
    AND left_friend.friend_public_key = $2
)
`, leftUserPublicKey, rightUserPublicKey)
	var exists bool
	if err := row.Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func (r *ClientRepository) ListFriends(ctx context.Context, userPublicKey []byte, limit int, offset int) ([]clientapi.FriendRecord, error) {
	rows, err := r.dbtx(ctx).QueryContext(ctx, `SELECT id, user_public_key, friend_public_key, accepted_at FROM friends WHERE user_public_key = $1 ORDER BY accepted_at DESC, id DESC LIMIT $2 OFFSET $3`, userPublicKey, limit, offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var items []clientapi.FriendRecord
	for rows.Next() {
		var item clientapi.FriendRecord
		if err := rows.Scan(&item.ID, &item.UserPublicKey, &item.FriendPublicKey, &item.AcceptedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *ClientRepository) CountFriends(ctx context.Context, userPublicKey []byte) (int64, error) {
	var count int64
	if err := r.dbtx(ctx).QueryRowContext(ctx, `SELECT COUNT(*) FROM friends WHERE user_public_key = $1`, userPublicKey).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (r *ClientRepository) RemoveFriend(ctx context.Context, userPublicKey []byte, friendPublicKey []byte, removedAt time.Time) error {
	_, err := r.dbtx(ctx).ExecContext(ctx, `DELETE FROM friends WHERE (user_public_key = $1 AND friend_public_key = $2) OR (user_public_key = $2 AND friend_public_key = $1)`, userPublicKey, friendPublicKey)
	return err
}

func (r *ClientRepository) CreateFriendRequest(ctx context.Context, request clientapi.FriendRequestRecord) error {
	_, err := r.dbtx(ctx).ExecContext(ctx, `INSERT INTO friend_requests (request_id, sender_public_key, receiver_public_key, state, created_at, updated_at) VALUES ($1,$2,$3,$4,$5,$6)`, request.RequestID, request.SenderPublicKey, request.ReceiverPublicKey, request.State, request.CreatedAt, request.UpdatedAt)
	return err
}

func (r *ClientRepository) UpdateFriendRequestState(ctx context.Context, requestID uuid.UUID, actorPublicKey []byte, allowedFromStates []int16, targetState int16, updatedAt time.Time) (clientapi.FriendRequestRecord, error) {
	record, err := r.GetFriendRequest(ctx, requestID)
	if err != nil {
		return clientapi.FriendRequestRecord{}, err
	}
	if string(actorPublicKey) != string(record.SenderPublicKey) && string(actorPublicKey) != string(record.ReceiverPublicKey) {
		return clientapi.FriendRequestRecord{}, clientapi.ErrForbidden
	}
	if !containsState(allowedFromStates, record.State) {
		return clientapi.FriendRequestRecord{}, clientapi.ErrConflict
	}
	row := r.dbtx(ctx).QueryRowContext(ctx, `UPDATE friend_requests SET state = $2, updated_at = $3 WHERE request_id = $1 RETURNING request_id, sender_public_key, receiver_public_key, state, created_at, updated_at`, requestID, targetState, updatedAt)
	var out clientapi.FriendRequestRecord
	if err := row.Scan(&out.RequestID, &out.SenderPublicKey, &out.ReceiverPublicKey, &out.State, &out.CreatedAt, &out.UpdatedAt); err != nil {
		return clientapi.FriendRequestRecord{}, err
	}
	return out, nil
}

func (r *ClientRepository) GetFriendRequest(ctx context.Context, requestID uuid.UUID) (clientapi.FriendRequestRecord, error) {
	row := r.dbtx(ctx).QueryRowContext(ctx, `SELECT request_id, sender_public_key, receiver_public_key, state, created_at, updated_at FROM friend_requests WHERE request_id = $1`, requestID)
	var out clientapi.FriendRequestRecord
	if err := row.Scan(&out.RequestID, &out.SenderPublicKey, &out.ReceiverPublicKey, &out.State, &out.CreatedAt, &out.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return clientapi.FriendRequestRecord{}, clientapi.ErrNotFound
		}
		return clientapi.FriendRequestRecord{}, err
	}
	return out, nil
}

func (r *ClientRepository) ListFriendRequests(ctx context.Context, userPublicKey []byte, direction string, limit int, offset int) ([]clientapi.FriendRequestRecord, error) {
	query := `SELECT request_id, sender_public_key, receiver_public_key, state, created_at, updated_at FROM friend_requests WHERE (sender_public_key = $1 OR receiver_public_key = $1) ORDER BY created_at DESC, request_id DESC LIMIT $2 OFFSET $3`
	switch direction {
	case "incoming":
		query = `SELECT request_id, sender_public_key, receiver_public_key, state, created_at, updated_at FROM friend_requests WHERE receiver_public_key = $1 ORDER BY created_at DESC, request_id DESC LIMIT $2 OFFSET $3`
	case "outgoing":
		query = `SELECT request_id, sender_public_key, receiver_public_key, state, created_at, updated_at FROM friend_requests WHERE sender_public_key = $1 ORDER BY created_at DESC, request_id DESC LIMIT $2 OFFSET $3`
	}
	rows, err := r.dbtx(ctx).QueryContext(ctx, query, userPublicKey, limit, offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var items []clientapi.FriendRequestRecord
	for rows.Next() {
		var item clientapi.FriendRequestRecord
		if err := rows.Scan(&item.RequestID, &item.SenderPublicKey, &item.ReceiverPublicKey, &item.State, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *ClientRepository) CreateFriendPair(ctx context.Context, left clientapi.FriendRecord, right clientapi.FriendRecord) error {
	for _, item := range []clientapi.FriendRecord{left, right} {
		if _, err := r.dbtx(ctx).ExecContext(ctx, `INSERT INTO friends (id, user_public_key, friend_public_key, accepted_at) VALUES ($1,$2,$3,$4) ON CONFLICT (user_public_key, friend_public_key) DO NOTHING`, item.ID, item.UserPublicKey, item.FriendPublicKey, item.AcceptedAt); err != nil {
			return err
		}
	}
	return nil
}

func (r *ClientRepository) CreateRoom(ctx context.Context, room clientapi.ChatRoomRecord, owner clientapi.ChatMemberRecord) error {
	if _, err := r.dbtx(ctx).ExecContext(ctx, `INSERT INTO chat_rooms (room_id, owner_public_key, title, description, visibility, avatar_hash, avatar_bytes, avatar_content_type, state_id, created_at, updated_at) VALUES ($1,$2,$3,NULLIF($4,''),$5,NULLIF($6,''),$7,$8,$9,$10,$11)`, room.RoomID, room.OwnerPublicKey, room.Title, room.Description, room.Visibility, room.AvatarHash, room.AvatarBytes, room.AvatarContentType, room.StateID, room.CreatedAt, room.UpdatedAt); err != nil {
		return err
	}
	if _, err := r.dbtx(ctx).ExecContext(ctx, `INSERT INTO chat_members (room_id, user_public_key, role, notification_level, joined_at) VALUES ($1,$2,$3,$4,$5)`, owner.RoomID, owner.UserPublicKey, owner.Role, owner.NotificationLevel, owner.JoinedAt); err != nil {
		return err
	}
	return nil
}

func (r *ClientRepository) ListRooms(ctx context.Context, userPublicKey []byte, limit int, offset int) ([]clientapi.ChatRoomRecord, error) {
	rows, err := r.dbtx(ctx).QueryContext(ctx, `
SELECT r.room_id, r.owner_public_key, r.title, COALESCE(r.description,''), r.visibility, COALESCE(r.avatar_hash,''), COALESCE(r.avatar_bytes,''::bytea), COALESCE(r.avatar_content_type,''), r.state_id, r.created_at, r.updated_at, r.deleted_at
FROM chat_rooms r
JOIN chat_members m ON m.room_id = r.room_id AND m.left_at IS NULL
WHERE m.user_public_key = $1 AND r.deleted_at IS NULL
ORDER BY r.updated_at DESC, r.room_id DESC LIMIT $2 OFFSET $3
`, userPublicKey, limit, offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanRooms(rows)
}

func (r *ClientRepository) GetRoom(ctx context.Context, roomID uuid.UUID) (clientapi.ChatRoomRecord, error) {
	row := r.dbtx(ctx).QueryRowContext(ctx, `SELECT room_id, owner_public_key, title, COALESCE(description,''), visibility, COALESCE(avatar_hash,''), COALESCE(avatar_bytes,''::bytea), COALESCE(avatar_content_type,''), state_id, created_at, updated_at, deleted_at FROM chat_rooms WHERE room_id = $1 AND deleted_at IS NULL`, roomID)
	var out clientapi.ChatRoomRecord
	if err := row.Scan(&out.RoomID, &out.OwnerPublicKey, &out.Title, &out.Description, &out.Visibility, &out.AvatarHash, &out.AvatarBytes, &out.AvatarContentType, &out.StateID, &out.CreatedAt, &out.UpdatedAt, &out.DeletedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return clientapi.ChatRoomRecord{}, clientapi.ErrNotFound
		}
		return clientapi.ChatRoomRecord{}, err
	}
	return out, nil
}

func (r *ClientRepository) SearchRooms(ctx context.Context, query string, limit int, offset int) ([]clientapi.ChatRoomRecord, error) {
	rows, err := r.dbtx(ctx).QueryContext(ctx, `SELECT room_id, owner_public_key, title, COALESCE(description,''), visibility, COALESCE(avatar_hash,''), COALESCE(avatar_bytes,''::bytea), COALESCE(avatar_content_type,''), state_id, created_at, updated_at, deleted_at FROM chat_rooms WHERE deleted_at IS NULL AND visibility = $1 AND POSITION(LOWER($2) IN LOWER(title)) > 0 ORDER BY updated_at DESC, room_id DESC LIMIT $3 OFFSET $4`, clientapi.VisibilityPublic, query, limit, offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanRooms(rows)
}

func (r *ClientRepository) UpdateRoom(ctx context.Context, userPublicKey []byte, roomID uuid.UUID, title string, description string, avatarHash string, updatedAt time.Time) error {
	room, err := r.GetRoom(ctx, roomID)
	if err != nil {
		return err
	}
	if string(room.OwnerPublicKey) != string(userPublicKey) {
		return clientapi.ErrForbidden
	}
	_, err = r.dbtx(ctx).ExecContext(ctx, `UPDATE chat_rooms SET title = COALESCE(NULLIF($3,''), title), description = COALESCE(NULLIF($4,''), description), avatar_hash = COALESCE(NULLIF($5,''), avatar_hash), updated_at = $2 WHERE room_id = $1`, roomID, updatedAt, title, description, avatarHash)
	return err
}

func (r *ClientRepository) DeleteRoom(ctx context.Context, userPublicKey []byte, roomID uuid.UUID, deletedAt time.Time) error {
	room, err := r.GetRoom(ctx, roomID)
	if err != nil {
		return err
	}
	if string(room.OwnerPublicKey) != string(userPublicKey) {
		return clientapi.ErrForbidden
	}
	_, err = r.dbtx(ctx).ExecContext(ctx, `UPDATE chat_rooms SET deleted_at = $2, updated_at = $2 WHERE room_id = $1`, roomID, deletedAt)
	return err
}

func (r *ClientRepository) GetRoomAvatar(ctx context.Context, roomID uuid.UUID) (clientapi.AvatarRecord, error) {
	row := r.dbtx(ctx).QueryRowContext(ctx, `SELECT COALESCE(avatar_bytes,''::bytea), COALESCE(avatar_content_type,'') FROM chat_rooms WHERE room_id = $1 AND deleted_at IS NULL`, roomID)
	var out clientapi.AvatarRecord
	if err := row.Scan(&out.Bytes, &out.ContentType); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return clientapi.AvatarRecord{}, clientapi.ErrNotFound
		}
		return clientapi.AvatarRecord{}, err
	}
	return out, nil
}

func (r *ClientRepository) AddRoomState(ctx context.Context, userPublicKey []byte, state clientapi.ChatRoomStateRecord) error {
	if _, err := r.ensureMember(ctx, state.RoomID, userPublicKey); err != nil {
		return err
	}
	if _, err := r.dbtx(ctx).ExecContext(ctx, `INSERT INTO chat_room_states (id, room_id, group_id, epoch, tree_bytes, tree_hash, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7)`, state.ID, state.RoomID, state.GroupID, state.Epoch, state.TreeBytes, state.TreeHash, state.CreatedAt); err != nil {
		return err
	}
	if _, err := r.dbtx(ctx).ExecContext(ctx, `UPDATE chat_rooms SET state_id = $2, updated_at = $3 WHERE room_id = $1`, state.RoomID, state.ID, state.CreatedAt); err != nil {
		return err
	}
	return nil
}

func (r *ClientRepository) FetchRoomState(ctx context.Context, userPublicKey []byte, roomID uuid.UUID, epoch int64) (clientapi.ChatRoomStateRecord, error) {
	if _, err := r.ensureMember(ctx, roomID, userPublicKey); err != nil {
		return clientapi.ChatRoomStateRecord{}, err
	}
	row := r.dbtx(ctx).QueryRowContext(ctx, `SELECT id, room_id, group_id, epoch, tree_bytes, tree_hash, created_at FROM chat_room_states WHERE room_id = $1 AND epoch = $2`, roomID, epoch)
	var out clientapi.ChatRoomStateRecord
	if err := row.Scan(&out.ID, &out.RoomID, &out.GroupID, &out.Epoch, &out.TreeBytes, &out.TreeHash, &out.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return clientapi.ChatRoomStateRecord{}, clientapi.ErrNotFound
		}
		return clientapi.ChatRoomStateRecord{}, err
	}
	return out, nil
}

func (r *ClientRepository) JoinRoom(ctx context.Context, member clientapi.ChatMemberRecord) error {
	room, err := r.GetRoom(ctx, member.RoomID)
	if err != nil {
		return err
	}
	if room.Visibility == clientapi.VisibilityPrivate {
		return clientapi.ErrForbidden
	}
	_, err = r.dbtx(ctx).ExecContext(ctx, `INSERT INTO chat_members (room_id, user_public_key, role, notification_level, joined_at, left_at) VALUES ($1,$2,$3,$4,$5,NULL) ON CONFLICT (room_id, user_public_key) DO UPDATE SET left_at = NULL, role = EXCLUDED.role, notification_level = EXCLUDED.notification_level`, member.RoomID, member.UserPublicKey, member.Role, member.NotificationLevel, member.JoinedAt)
	return err
}

func (r *ClientRepository) UpsertRoomMembership(ctx context.Context, member clientapi.ChatMemberRecord) error {
	if _, err := r.GetRoom(ctx, member.RoomID); err != nil {
		return err
	}
	_, err := r.dbtx(ctx).ExecContext(ctx, `INSERT INTO chat_members (room_id, user_public_key, role, notification_level, joined_at, left_at) VALUES ($1,$2,$3,$4,$5,NULL) ON CONFLICT (room_id, user_public_key) DO UPDATE SET left_at = NULL, role = EXCLUDED.role, notification_level = EXCLUDED.notification_level`, member.RoomID, member.UserPublicKey, member.Role, member.NotificationLevel, member.JoinedAt)
	return err
}

func (r *ClientRepository) LeaveRoom(ctx context.Context, roomID uuid.UUID, userPublicKey []byte, leftAt time.Time) error {
	res, err := r.dbtx(ctx).ExecContext(ctx, `UPDATE chat_members SET left_at = $3 WHERE room_id = $1 AND user_public_key = $2 AND left_at IS NULL`, roomID, userPublicKey, leftAt)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return clientapi.ErrNotFound
	}
	return nil
}

func (r *ClientRepository) KickMember(ctx context.Context, actorPublicKey []byte, roomID uuid.UUID, targetPublicKey []byte, kickedAt time.Time) error {
	if err := r.ensureRoomAdmin(ctx, roomID, actorPublicKey); err != nil {
		return err
	}
	_, err := r.dbtx(ctx).ExecContext(ctx, `UPDATE chat_members SET left_at = $3 WHERE room_id = $1 AND user_public_key = $2 AND left_at IS NULL`, roomID, targetPublicKey, kickedAt)
	return err
}

func (r *ClientRepository) ListMembers(ctx context.Context, roomID uuid.UUID, limit int, offset int) ([]clientapi.ChatMemberRecord, error) {
	rows, err := r.dbtx(ctx).QueryContext(ctx, `SELECT room_id, user_public_key, role, notification_level, joined_at, left_at FROM chat_members WHERE room_id = $1 AND left_at IS NULL ORDER BY joined_at ASC, user_public_key ASC LIMIT $2 OFFSET $3`, roomID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var items []clientapi.ChatMemberRecord
	for rows.Next() {
		var item clientapi.ChatMemberRecord
		if err := rows.Scan(&item.RoomID, &item.UserPublicKey, &item.Role, &item.NotificationLevel, &item.JoinedAt, &item.LeftAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *ClientRepository) UpdateMemberRole(ctx context.Context, actorPublicKey []byte, roomID uuid.UUID, targetPublicKey []byte, role int16, updatedAt time.Time) error {
	if err := r.ensureRoomAdmin(ctx, roomID, actorPublicKey); err != nil {
		return err
	}
	_, err := r.dbtx(ctx).ExecContext(ctx, `UPDATE chat_members SET role = $3 WHERE room_id = $1 AND user_public_key = $2 AND left_at IS NULL`, roomID, targetPublicKey, role)
	return err
}

func (r *ClientRepository) CreateMemberPermission(ctx context.Context, actorPublicKey []byte, permission clientapi.ChatMemberPermissionRecord) error {
	if err := r.ensureRoomAdmin(ctx, permission.RoomID, actorPublicKey); err != nil {
		return err
	}
	_, err := r.dbtx(ctx).ExecContext(ctx, `INSERT INTO chat_member_permissions (permission_id, room_id, user_public_key, permission_key, is_allowed, created_at, updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (room_id, user_public_key, permission_key) DO UPDATE SET is_allowed = EXCLUDED.is_allowed, updated_at = EXCLUDED.updated_at`, permission.PermissionID, permission.RoomID, permission.UserPublicKey, permission.PermissionKey, permission.IsAllowed, permission.CreatedAt, permission.UpdatedAt)
	return err
}

func (r *ClientRepository) ListMemberPermissions(ctx context.Context, roomID uuid.UUID, userPublicKey []byte, limit int, offset int) ([]clientapi.ChatMemberPermissionRecord, error) {
	query := `SELECT permission_id, room_id, user_public_key, permission_key, is_allowed, created_at, updated_at FROM chat_member_permissions WHERE room_id = $1 ORDER BY created_at DESC, permission_id DESC LIMIT $2 OFFSET $3`
	args := []any{roomID, limit, offset}
	if len(userPublicKey) == 32 {
		query = `SELECT permission_id, room_id, user_public_key, permission_key, is_allowed, created_at, updated_at FROM chat_member_permissions WHERE room_id = $1 AND user_public_key = $2 ORDER BY created_at DESC, permission_id DESC LIMIT $3 OFFSET $4`
		args = []any{roomID, userPublicKey, limit, offset}
	}
	rows, err := r.dbtx(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var items []clientapi.ChatMemberPermissionRecord
	for rows.Next() {
		var item clientapi.ChatMemberPermissionRecord
		if err := rows.Scan(&item.PermissionID, &item.RoomID, &item.UserPublicKey, &item.PermissionKey, &item.IsAllowed, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *ClientRepository) UpdateMemberPermission(ctx context.Context, actorPublicKey []byte, permissionID uuid.UUID, isAllowed bool, updatedAt time.Time) error {
	roomID, err := r.lookupPermissionRoom(ctx, permissionID)
	if err != nil {
		return err
	}
	if err := r.ensureRoomAdmin(ctx, roomID, actorPublicKey); err != nil {
		return err
	}
	_, err = r.dbtx(ctx).ExecContext(ctx, `UPDATE chat_member_permissions SET is_allowed = $2, updated_at = $3 WHERE permission_id = $1`, permissionID, isAllowed, updatedAt)
	return err
}

func (r *ClientRepository) DeleteMemberPermission(ctx context.Context, actorPublicKey []byte, permissionID uuid.UUID, deletedAt time.Time) error {
	roomID, err := r.lookupPermissionRoom(ctx, permissionID)
	if err != nil {
		return err
	}
	if err := r.ensureRoomAdmin(ctx, roomID, actorPublicKey); err != nil {
		return err
	}
	_, err = r.dbtx(ctx).ExecContext(ctx, `DELETE FROM chat_member_permissions WHERE permission_id = $1`, permissionID)
	return err
}

func (r *ClientRepository) lookupPermissionRoom(ctx context.Context, permissionID uuid.UUID) (uuid.UUID, error) {
	row := r.dbtx(ctx).QueryRowContext(ctx, `SELECT room_id FROM chat_member_permissions WHERE permission_id = $1`, permissionID)
	var roomID uuid.UUID
	if err := row.Scan(&roomID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return uuid.Nil, clientapi.ErrNotFound
		}
		return uuid.Nil, err
	}
	return roomID, nil
}

func (r *ClientRepository) CreateInvitation(ctx context.Context, invitation clientapi.ChatInvitationRecord) error {
	if err := r.ensureRoomAdmin(ctx, invitation.RoomID, invitation.InviterPublicKey); err != nil {
		return err
	}
	_, err := r.dbtx(ctx).ExecContext(ctx, `INSERT INTO chat_invitations (invitation_id, room_id, inviter_public_key, invitee_public_key, expires_at, invite_token, invite_token_signature, state, created_at, updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, invitation.InvitationID, invitation.RoomID, invitation.InviterPublicKey, invitation.InviteePublicKey, invitation.ExpiresAt, nullIfEmptyBytes(invitation.InviteToken), nullIfEmptyBytes(invitation.InviteTokenSignature), invitation.State, invitation.CreatedAt, invitation.UpdatedAt)
	return err
}

func (r *ClientRepository) GetInvitation(ctx context.Context, invitationID uuid.UUID) (clientapi.ChatInvitationRecord, error) {
	return r.getInvitation(ctx, invitationID)
}

func (r *ClientRepository) ListSentInvitations(ctx context.Context, inviterPublicKey []byte, roomID *uuid.UUID, limit int, offset int) ([]clientapi.ChatInvitationRecord, error) {
	query := `SELECT invitation_id, room_id, inviter_public_key, invitee_public_key, expires_at, COALESCE(invite_token, ''::bytea), COALESCE(invite_token_signature, ''::bytea), state, created_at, updated_at FROM chat_invitations WHERE inviter_public_key = $1 ORDER BY created_at DESC, invitation_id DESC LIMIT $2 OFFSET $3`
	args := []any{inviterPublicKey, limit, offset}
	if roomID != nil {
		query = `SELECT invitation_id, room_id, inviter_public_key, invitee_public_key, expires_at, COALESCE(invite_token, ''::bytea), COALESCE(invite_token_signature, ''::bytea), state, created_at, updated_at FROM chat_invitations WHERE inviter_public_key = $1 AND room_id = $2 ORDER BY created_at DESC, invitation_id DESC LIMIT $3 OFFSET $4`
		args = []any{inviterPublicKey, *roomID, limit, offset}
	}
	rows, err := r.dbtx(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanInvitations(rows)
}

func (r *ClientRepository) ListIncomingInvitations(ctx context.Context, inviteePublicKey []byte, limit int, offset int) ([]clientapi.ChatInvitationRecord, error) {
	rows, err := r.dbtx(ctx).QueryContext(ctx, `SELECT invitation_id, room_id, inviter_public_key, invitee_public_key, expires_at, COALESCE(invite_token, ''::bytea), COALESCE(invite_token_signature, ''::bytea), state, created_at, updated_at FROM chat_invitations WHERE invitee_public_key = $1 ORDER BY created_at DESC, invitation_id DESC LIMIT $2 OFFSET $3`, inviteePublicKey, limit, offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanInvitations(rows)
}

func (r *ClientRepository) UpdateInvitationState(ctx context.Context, invitationID uuid.UUID, actorPublicKey []byte, targetState int16, updatedAt time.Time, allowedCurrentStates []int16) (clientapi.ChatInvitationRecord, error) {
	record, err := r.getInvitation(ctx, invitationID)
	if err != nil {
		return clientapi.ChatInvitationRecord{}, err
	}
	if string(actorPublicKey) != string(record.InviterPublicKey) && string(actorPublicKey) != string(record.InviteePublicKey) {
		return clientapi.ChatInvitationRecord{}, clientapi.ErrForbidden
	}
	if !containsState(allowedCurrentStates, record.State) {
		return clientapi.ChatInvitationRecord{}, clientapi.ErrConflict
	}
	row := r.dbtx(ctx).QueryRowContext(ctx, `UPDATE chat_invitations SET state = $2, updated_at = $3 WHERE invitation_id = $1 RETURNING invitation_id, room_id, inviter_public_key, invitee_public_key, expires_at, COALESCE(invite_token, ''::bytea), COALESCE(invite_token_signature, ''::bytea), state, created_at, updated_at`, invitationID, targetState, updatedAt)
	var out clientapi.ChatInvitationRecord
	if err := row.Scan(&out.InvitationID, &out.RoomID, &out.InviterPublicKey, &out.InviteePublicKey, &out.ExpiresAt, &out.InviteToken, &out.InviteTokenSignature, &out.State, &out.CreatedAt, &out.UpdatedAt); err != nil {
		return clientapi.ChatInvitationRecord{}, err
	}
	return out, nil
}

func (r *ClientRepository) getInvitation(ctx context.Context, invitationID uuid.UUID) (clientapi.ChatInvitationRecord, error) {
	row := r.dbtx(ctx).QueryRowContext(ctx, `SELECT invitation_id, room_id, inviter_public_key, invitee_public_key, expires_at, COALESCE(invite_token, ''::bytea), COALESCE(invite_token_signature, ''::bytea), state, created_at, updated_at FROM chat_invitations WHERE invitation_id = $1`, invitationID)
	var out clientapi.ChatInvitationRecord
	if err := row.Scan(&out.InvitationID, &out.RoomID, &out.InviterPublicKey, &out.InviteePublicKey, &out.ExpiresAt, &out.InviteToken, &out.InviteTokenSignature, &out.State, &out.CreatedAt, &out.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return clientapi.ChatInvitationRecord{}, clientapi.ErrNotFound
		}
		return clientapi.ChatInvitationRecord{}, err
	}
	return out, nil
}

func (r *ClientRepository) FindPendingInvitation(ctx context.Context, roomID uuid.UUID, inviteePublicKey []byte) (clientapi.ChatInvitationRecord, bool, error) {
	row := r.dbtx(ctx).QueryRowContext(ctx, `
SELECT invitation_id, room_id, inviter_public_key, invitee_public_key, expires_at, COALESCE(invite_token, ''::bytea), COALESCE(invite_token_signature, ''::bytea), state, created_at, updated_at
FROM chat_invitations
WHERE room_id = $1
  AND invitee_public_key = $2
  AND state = $3
ORDER BY created_at DESC
LIMIT 1
`, roomID, inviteePublicKey, clientapi.InvitationPending)
	var out clientapi.ChatInvitationRecord
	if err := row.Scan(&out.InvitationID, &out.RoomID, &out.InviterPublicKey, &out.InviteePublicKey, &out.ExpiresAt, &out.InviteToken, &out.InviteTokenSignature, &out.State, &out.CreatedAt, &out.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return clientapi.ChatInvitationRecord{}, false, nil
		}
		return clientapi.ChatInvitationRecord{}, false, err
	}
	return out, true, nil
}

func (r *ClientRepository) ListActiveRoomMemberPublicKeys(ctx context.Context, roomID uuid.UUID) ([][]byte, error) {
	rows, err := r.dbtx(ctx).QueryContext(ctx, `SELECT user_public_key FROM chat_members WHERE room_id = $1 AND left_at IS NULL`, roomID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var items [][]byte
	for rows.Next() {
		var value []byte
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		items = append(items, value)
	}
	return items, rows.Err()
}

func (r *ClientRepository) CountServerStats(ctx context.Context) (clientapi.ServerStats, error) {
	stats := clientapi.ServerStats{}
	queries := []struct {
		query string
		dst   *int64
	}{
		{`SELECT COUNT(*) FROM profiles WHERE deleted_at IS NULL`, &stats.Profiles},
		{`SELECT COUNT(*) FROM device_push_tokens`, &stats.Devices},
		{`SELECT COUNT(*) FROM friends`, &stats.Friends},
		{`SELECT COUNT(*) FROM chat_rooms WHERE deleted_at IS NULL`, &stats.Rooms},
	}
	for _, item := range queries {
		if err := r.dbtx(ctx).QueryRowContext(ctx, item.query).Scan(item.dst); err != nil {
			return clientapi.ServerStats{}, err
		}
	}
	return stats, nil
}

func (r *ClientRepository) CountUserStats(ctx context.Context, userPublicKey []byte) (clientapi.UserStats, error) {
	stats := clientapi.UserStats{}
	queries := []struct {
		query string
		dst   *int64
	}{
		{`SELECT COUNT(*) FROM device_push_tokens WHERE user_public_key = $1`, &stats.Devices},
		{`SELECT COUNT(*) FROM key_packages WHERE user_public_key = $1`, &stats.KeyPackages},
		{`SELECT COUNT(*) FROM friends WHERE user_public_key = $1`, &stats.Friends},
		{`SELECT COUNT(*) FROM friend_requests WHERE sender_public_key = $1 AND state = 1`, &stats.OutgoingFriendRequests},
		{`SELECT COUNT(*) FROM chat_members WHERE user_public_key = $1 AND left_at IS NULL`, &stats.Rooms},
	}
	for _, item := range queries {
		if err := r.dbtx(ctx).QueryRowContext(ctx, item.query, userPublicKey).Scan(item.dst); err != nil {
			return clientapi.UserStats{}, err
		}
	}
	return stats, nil
}

func (r *ClientRepository) CountGroupStats(ctx context.Context, roomID uuid.UUID) (clientapi.GroupStats, error) {
	stats := clientapi.GroupStats{}
	queries := []struct {
		query string
		dst   *int64
	}{
		{`SELECT COUNT(*) FROM chat_members WHERE room_id = $1 AND left_at IS NULL`, &stats.Members},
		{`SELECT COUNT(*) FROM chat_invitations WHERE room_id = $1`, &stats.Invites},
	}
	for _, item := range queries {
		if err := r.dbtx(ctx).QueryRowContext(ctx, item.query, roomID).Scan(item.dst); err != nil {
			return clientapi.GroupStats{}, err
		}
	}
	return stats, nil
}

func (r *ClientRepository) RecordUserUsage(ctx context.Context, userPublicKey []byte, now time.Time, requests int64, bytesIn int64, bytesOut int64) error {
	if requests == 0 && bytesIn == 0 && bytesOut == 0 {
		return nil
	}
	_, err := r.dbtx(ctx).ExecContext(ctx, `
WITH kinds(kind) AS (
  SELECT unnest(ARRAY['minute','hour','day','week','month','allTime']::text[])
)
INSERT INTO user_usage_stats (user_public_key, kind, bucket_start, requests, bytes_in, bytes_out, updated_at)
SELECT
  $1,
  k.kind,
  CASE k.kind
    WHEN 'minute' THEN (date_trunc('minute', $2 AT TIME ZONE 'UTC') AT TIME ZONE 'UTC')
    WHEN 'hour' THEN (date_trunc('hour', $2 AT TIME ZONE 'UTC') AT TIME ZONE 'UTC')
    WHEN 'day' THEN (date_trunc('day', $2 AT TIME ZONE 'UTC') AT TIME ZONE 'UTC')
    WHEN 'week' THEN (date_trunc('week', $2 AT TIME ZONE 'UTC') AT TIME ZONE 'UTC')
    WHEN 'month' THEN (date_trunc('month', $2 AT TIME ZONE 'UTC') AT TIME ZONE 'UTC')
    WHEN 'allTime' THEN TIMESTAMPTZ '1970-01-01 00:00:00+00'
  END,
  $3,
  $4,
  $5,
  $2
FROM kinds k
ON CONFLICT (user_public_key, kind, bucket_start)
DO UPDATE SET
  requests = user_usage_stats.requests + EXCLUDED.requests,
  bytes_in = user_usage_stats.bytes_in + EXCLUDED.bytes_in,
  bytes_out = user_usage_stats.bytes_out + EXCLUDED.bytes_out,
  updated_at = EXCLUDED.updated_at
`, userPublicKey, now, requests, bytesIn, bytesOut)
	if err != nil {
		return fmt.Errorf("record user usage: %w", err)
	}
	return nil
}

func (r *ClientRepository) GetUserUsageStats(ctx context.Context, userPublicKey []byte, now time.Time) (clientapi.UsageStats, error) {
	rows, err := r.dbtx(ctx).QueryContext(ctx, `
WITH targets(kind, bucket_start) AS (
  VALUES
    ('minute', (date_trunc('minute', $2 AT TIME ZONE 'UTC') AT TIME ZONE 'UTC')),
    ('hour', (date_trunc('hour', $2 AT TIME ZONE 'UTC') AT TIME ZONE 'UTC')),
    ('day', (date_trunc('day', $2 AT TIME ZONE 'UTC') AT TIME ZONE 'UTC')),
    ('week', (date_trunc('week', $2 AT TIME ZONE 'UTC') AT TIME ZONE 'UTC')),
    ('month', (date_trunc('month', $2 AT TIME ZONE 'UTC') AT TIME ZONE 'UTC')),
    ('allTime', TIMESTAMPTZ '1970-01-01 00:00:00+00')
)
SELECT
  t.kind,
  COALESCE(s.requests, 0) AS requests,
  COALESCE(s.bytes_in, 0) AS bytes_in,
  COALESCE(s.bytes_out, 0) AS bytes_out
FROM targets t
LEFT JOIN user_usage_stats s
  ON s.user_public_key = $1
 AND s.kind = t.kind
 AND s.bucket_start = t.bucket_start
`, userPublicKey, now)
	if err != nil {
		return clientapi.UsageStats{}, fmt.Errorf("get user usage stats: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := clientapi.UsageStats{}
	for rows.Next() {
		var kind string
		var stat clientapi.UsageStat
		if err := rows.Scan(&kind, &stat.Requests, &stat.BytesIn, &stat.BytesOut); err != nil {
			return clientapi.UsageStats{}, fmt.Errorf("scan user usage stats: %w", err)
		}
		switch kind {
		case "minute":
			out.Minute = stat
		case "hour":
			out.Hour = stat
		case "day":
			out.Day = stat
		case "week":
			out.Week = stat
		case "month":
			out.Month = stat
		case "allTime":
			out.AllTime = stat
		}
	}
	if err := rows.Err(); err != nil {
		return clientapi.UsageStats{}, fmt.Errorf("iterate user usage stats: %w", err)
	}
	return out, nil
}

func (r *ClientRepository) ensureMember(ctx context.Context, roomID uuid.UUID, userPublicKey []byte) (clientapi.ChatMemberRecord, error) {
	row := r.dbtx(ctx).QueryRowContext(ctx, `SELECT room_id, user_public_key, role, notification_level, joined_at, left_at FROM chat_members WHERE room_id = $1 AND user_public_key = $2 AND left_at IS NULL`, roomID, userPublicKey)
	var member clientapi.ChatMemberRecord
	if err := row.Scan(&member.RoomID, &member.UserPublicKey, &member.Role, &member.NotificationLevel, &member.JoinedAt, &member.LeftAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return clientapi.ChatMemberRecord{}, clientapi.ErrForbidden
		}
		return clientapi.ChatMemberRecord{}, err
	}
	return member, nil
}

func (r *ClientRepository) ensureRoomAdmin(ctx context.Context, roomID uuid.UUID, userPublicKey []byte) error {
	member, err := r.ensureMember(ctx, roomID, userPublicKey)
	if err != nil {
		return err
	}
	if member.Role < clientapi.RoleAdmin {
		return clientapi.ErrForbidden
	}
	return nil
}

func orderedPublicKeyPair(left []byte, right []byte) ([]byte, []byte) {
	if bytes.Compare(left, right) <= 0 {
		return append([]byte(nil), left...), append([]byte(nil), right...)
	}
	return append([]byte(nil), right...), append([]byte(nil), left...)
}

var _ clientapi.Store = (*ClientRepository)(nil)
