CREATE TABLE IF NOT EXISTS chat_room_welcomes (
  room_id UUID NOT NULL REFERENCES chat_rooms(room_id) ON DELETE CASCADE,
  target_user_public_key BYTEA NOT NULL,
  sender_public_key BYTEA NOT NULL,
  welcome_bytes BYTEA NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (room_id, target_user_public_key),
  CONSTRAINT chat_room_welcomes_target_user_public_key_length CHECK (octet_length(target_user_public_key) = 32),
  CONSTRAINT chat_room_welcomes_sender_public_key_length CHECK (octet_length(sender_public_key) = 32),
  CONSTRAINT chat_room_welcomes_welcome_bytes_non_empty CHECK (octet_length(welcome_bytes) > 0)
);

CREATE INDEX IF NOT EXISTS chat_room_welcomes_updated_at_idx
  ON chat_room_welcomes (updated_at DESC);
