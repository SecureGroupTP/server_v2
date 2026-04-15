CREATE TABLE IF NOT EXISTS user_usage_stats (
  user_public_key BYTEA NOT NULL REFERENCES profiles(public_key) ON DELETE CASCADE,
  kind TEXT NOT NULL,
  bucket_start TIMESTAMPTZ NOT NULL,
  requests BIGINT NOT NULL DEFAULT 0,
  bytes_in BIGINT NOT NULL DEFAULT 0,
  bytes_out BIGINT NOT NULL DEFAULT 0,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (user_public_key, kind, bucket_start),
  CONSTRAINT user_usage_stats_user_public_key_length CHECK (octet_length(user_public_key) = 32),
  CONSTRAINT user_usage_stats_kind_valid CHECK (kind IN ('minute','hour','day','week','month','allTime')),
  CONSTRAINT user_usage_stats_non_negative CHECK (requests >= 0 AND bytes_in >= 0 AND bytes_out >= 0)
);

CREATE INDEX IF NOT EXISTS user_usage_stats_user_kind_bucket_desc_idx
  ON user_usage_stats (user_public_key, kind, bucket_start DESC);
