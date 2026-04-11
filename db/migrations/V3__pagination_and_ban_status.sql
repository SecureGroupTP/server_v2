CREATE TABLE IF NOT EXISTS ban_statuses (
  public_key BYTEA PRIMARY KEY,
  is_banned BOOLEAN NOT NULL DEFAULT TRUE,
  reason TEXT,
  banned_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  expires_at TIMESTAMPTZ,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT ban_statuses_public_key_length CHECK (octet_length(public_key) = 32)
);

CREATE INDEX IF NOT EXISTS ban_statuses_active_idx
  ON ban_statuses (is_banned, expires_at, updated_at DESC);
