CREATE TABLE IF NOT EXISTS direct_rooms (
  room_id UUID PRIMARY KEY REFERENCES chat_rooms(room_id) ON DELETE CASCADE,
  left_user_public_key BYTEA NOT NULL,
  right_user_public_key BYTEA NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT direct_rooms_left_user_public_key_length CHECK (octet_length(left_user_public_key) = 32),
  CONSTRAINT direct_rooms_right_user_public_key_length CHECK (octet_length(right_user_public_key) = 32),
  CONSTRAINT direct_rooms_distinct_users CHECK (left_user_public_key <> right_user_public_key),
  UNIQUE (left_user_public_key, right_user_public_key)
);

CREATE INDEX IF NOT EXISTS direct_rooms_right_user_public_key_idx
  ON direct_rooms (right_user_public_key, created_at DESC);
