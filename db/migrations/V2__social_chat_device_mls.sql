CREATE TABLE IF NOT EXISTS device_push_tokens (
  id UUID PRIMARY KEY,
  session_id UUID REFERENCES auth_sessions(session_id) ON DELETE SET NULL,
  user_public_key BYTEA NOT NULL,
  device_id TEXT NOT NULL,
  platform SMALLINT NOT NULL,
  push_token TEXT NOT NULL,
  is_enabled BOOLEAN NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT device_push_tokens_user_public_key_length CHECK (octet_length(user_public_key) = 32),
  CONSTRAINT device_push_tokens_platform_positive CHECK (platform > 0),
  CONSTRAINT device_push_tokens_push_token_non_empty CHECK (char_length(push_token) > 0),
  UNIQUE (user_public_key, device_id)
);

CREATE INDEX IF NOT EXISTS device_push_tokens_user_public_key_idx
  ON device_push_tokens (user_public_key, updated_at DESC);

CREATE TABLE IF NOT EXISTS key_packages (
  id UUID PRIMARY KEY,
  user_public_key BYTEA NOT NULL,
  device_id TEXT NOT NULL,
  key_package_bytes BYTEA NOT NULL,
  is_last_resort BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  expires_at TIMESTAMPTZ NOT NULL,
  CONSTRAINT key_packages_user_public_key_length CHECK (octet_length(user_public_key) = 32),
  CONSTRAINT key_packages_key_package_bytes_non_empty CHECK (octet_length(key_package_bytes) > 0)
);

CREATE INDEX IF NOT EXISTS key_packages_lookup_idx
  ON key_packages (user_public_key, device_id, expires_at);

CREATE TABLE IF NOT EXISTS friend_requests (
  request_id UUID PRIMARY KEY,
  sender_public_key BYTEA NOT NULL,
  receiver_public_key BYTEA NOT NULL,
  state SMALLINT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT friend_requests_sender_public_key_length CHECK (octet_length(sender_public_key) = 32),
  CONSTRAINT friend_requests_receiver_public_key_length CHECK (octet_length(receiver_public_key) = 32),
  CONSTRAINT friend_requests_state_positive CHECK (state > 0)
);

CREATE INDEX IF NOT EXISTS friend_requests_sender_idx
  ON friend_requests (sender_public_key, created_at DESC);

CREATE INDEX IF NOT EXISTS friend_requests_receiver_idx
  ON friend_requests (receiver_public_key, created_at DESC);

CREATE TABLE IF NOT EXISTS friends (
  id UUID PRIMARY KEY,
  user_public_key BYTEA NOT NULL,
  friend_public_key BYTEA NOT NULL,
  accepted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT friends_user_public_key_length CHECK (octet_length(user_public_key) = 32),
  CONSTRAINT friends_friend_public_key_length CHECK (octet_length(friend_public_key) = 32),
  UNIQUE (user_public_key, friend_public_key)
);

CREATE INDEX IF NOT EXISTS friends_user_public_key_idx
  ON friends (user_public_key, accepted_at DESC);

CREATE TABLE IF NOT EXISTS chat_rooms (
  room_id UUID PRIMARY KEY,
  owner_public_key BYTEA NOT NULL,
  title TEXT NOT NULL,
  description TEXT,
  visibility SMALLINT NOT NULL,
  avatar_hash TEXT,
  avatar_bytes BYTEA,
  avatar_content_type TEXT,
  state_id UUID,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at TIMESTAMPTZ,
  CONSTRAINT chat_rooms_owner_public_key_length CHECK (octet_length(owner_public_key) = 32),
  CONSTRAINT chat_rooms_visibility_positive CHECK (visibility > 0),
  CONSTRAINT chat_rooms_title_non_empty CHECK (char_length(title) > 0)
);

CREATE INDEX IF NOT EXISTS chat_rooms_updated_at_idx
  ON chat_rooms (updated_at DESC)
  WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS chat_room_states (
  id UUID PRIMARY KEY,
  room_id UUID NOT NULL REFERENCES chat_rooms(room_id) ON DELETE CASCADE,
  group_id UUID NOT NULL,
  epoch BIGINT NOT NULL,
  tree_bytes BYTEA NOT NULL,
  tree_hash BYTEA NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (room_id, epoch)
);

CREATE INDEX IF NOT EXISTS chat_room_states_room_id_idx
  ON chat_room_states (room_id, epoch DESC);

CREATE TABLE IF NOT EXISTS chat_members (
  room_id UUID NOT NULL REFERENCES chat_rooms(room_id) ON DELETE CASCADE,
  user_public_key BYTEA NOT NULL,
  role SMALLINT NOT NULL,
  notification_level SMALLINT NOT NULL DEFAULT 1,
  joined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  left_at TIMESTAMPTZ,
  PRIMARY KEY (room_id, user_public_key),
  CONSTRAINT chat_members_user_public_key_length CHECK (octet_length(user_public_key) = 32),
  CONSTRAINT chat_members_role_positive CHECK (role > 0),
  CONSTRAINT chat_members_notification_level_positive CHECK (notification_level > 0)
);

CREATE INDEX IF NOT EXISTS chat_members_user_public_key_idx
  ON chat_members (user_public_key, joined_at DESC)
  WHERE left_at IS NULL;

CREATE TABLE IF NOT EXISTS chat_member_permissions (
  permission_id UUID PRIMARY KEY,
  room_id UUID NOT NULL REFERENCES chat_rooms(room_id) ON DELETE CASCADE,
  user_public_key BYTEA NOT NULL,
  permission_key TEXT NOT NULL,
  is_allowed BOOLEAN NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT chat_member_permissions_user_public_key_length CHECK (octet_length(user_public_key) = 32),
  UNIQUE (room_id, user_public_key, permission_key)
);

CREATE INDEX IF NOT EXISTS chat_member_permissions_room_id_idx
  ON chat_member_permissions (room_id, created_at DESC);

CREATE TABLE IF NOT EXISTS chat_invitations (
  invitation_id UUID PRIMARY KEY,
  room_id UUID NOT NULL REFERENCES chat_rooms(room_id) ON DELETE CASCADE,
  inviter_public_key BYTEA NOT NULL,
  invitee_public_key BYTEA NOT NULL,
  state SMALLINT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT chat_invitations_inviter_public_key_length CHECK (octet_length(inviter_public_key) = 32),
  CONSTRAINT chat_invitations_invitee_public_key_length CHECK (octet_length(invitee_public_key) = 32),
  CONSTRAINT chat_invitations_state_positive CHECK (state > 0)
);

CREATE INDEX IF NOT EXISTS chat_invitations_inviter_idx
  ON chat_invitations (inviter_public_key, created_at DESC);

CREATE INDEX IF NOT EXISTS chat_invitations_invitee_idx
  ON chat_invitations (invitee_public_key, created_at DESC);

CREATE TABLE IF NOT EXISTS chat_messages (
  message_id UUID PRIMARY KEY,
  room_id UUID NOT NULL REFERENCES chat_rooms(room_id) ON DELETE CASCADE,
  sender_public_key BYTEA NOT NULL,
  client_msg_id UUID NOT NULL,
  body JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at TIMESTAMPTZ,
  CONSTRAINT chat_messages_sender_public_key_length CHECK (octet_length(sender_public_key) = 32),
  UNIQUE (room_id, sender_public_key, client_msg_id)
);

CREATE INDEX IF NOT EXISTS chat_messages_room_id_idx
  ON chat_messages (room_id, created_at DESC)
  WHERE deleted_at IS NULL;
