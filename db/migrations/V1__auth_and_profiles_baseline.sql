CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS profiles (
  public_key BYTEA PRIMARY KEY,
  username TEXT,
  display_name TEXT,
  bio TEXT,
  avatar_hash TEXT,
  avatar_bytes BYTEA,
  avatar_content_type TEXT,
  last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS profiles_username_unique_not_null
  ON profiles (LOWER(username))
  WHERE username IS NOT NULL;

CREATE TABLE IF NOT EXISTS auth_sessions (
  session_id UUID PRIMARY KEY,
  user_public_key BYTEA NOT NULL,
  claimed_public_ip INET,
  device_id TEXT NOT NULL,
  client_nonce BYTEA NOT NULL,
  challenge_payload BYTEA NOT NULL,
  is_authenticated BOOLEAN NOT NULL DEFAULT FALSE,
  authenticated_at TIMESTAMPTZ,
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT auth_sessions_user_public_key_length CHECK (octet_length(user_public_key) = 32),
  CONSTRAINT auth_sessions_client_nonce_non_empty CHECK (octet_length(client_nonce) > 0),
  CONSTRAINT auth_sessions_challenge_payload_non_empty CHECK (octet_length(challenge_payload) > 0)
);

CREATE INDEX IF NOT EXISTS auth_sessions_user_public_key_idx
  ON auth_sessions (user_public_key, created_at DESC);

CREATE INDEX IF NOT EXISTS auth_sessions_expires_at_idx
  ON auth_sessions (expires_at);

CREATE TABLE IF NOT EXISTS event_subscriptions (
  subscription_id UUID PRIMARY KEY,
  session_id UUID NOT NULL REFERENCES auth_sessions(session_id) ON DELETE CASCADE,
  user_public_key BYTEA NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  unsubscribed_at TIMESTAMPTZ,
  CONSTRAINT event_subscriptions_user_public_key_length CHECK (octet_length(user_public_key) = 32)
);

CREATE INDEX IF NOT EXISTS event_subscriptions_user_public_key_idx
  ON event_subscriptions (user_public_key, created_at DESC)
  WHERE unsubscribed_at IS NULL;

CREATE TABLE IF NOT EXISTS user_events (
  event_id UUID PRIMARY KEY,
  user_public_key BYTEA NOT NULL,
  reply_to_request_id UUID,
  event_type TEXT NOT NULL,
  payload JSONB NOT NULL DEFAULT '{}'::JSONB,
  available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  expires_at TIMESTAMPTZ NOT NULL,
  delivered_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT user_events_user_public_key_length CHECK (octet_length(user_public_key) = 32)
);

CREATE INDEX IF NOT EXISTS user_events_pending_idx
  ON user_events (user_public_key, available_at, created_at)
  WHERE delivered_at IS NULL;

CREATE INDEX IF NOT EXISTS user_events_expires_at_idx
  ON user_events (expires_at);
