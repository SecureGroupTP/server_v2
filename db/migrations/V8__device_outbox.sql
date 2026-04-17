CREATE TABLE IF NOT EXISTS outbox (
  event_id UUID PRIMARY KEY,
  device_id TEXT NOT NULL,
  segment_id TEXT NOT NULL,
  event_type TEXT NOT NULL,
  payload BYTEA NOT NULL,
  status SMALLINT NOT NULL,
  send_after TIMESTAMPTZ NOT NULL,
  inflight_until TIMESTAMPTZ,
  attempts INT NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  acked_at TIMESTAMPTZ,
  last_sent_at TIMESTAMPTZ,
  last_error TEXT,
  CONSTRAINT outbox_device_id_non_empty CHECK (char_length(device_id) > 0),
  CONSTRAINT outbox_segment_id_non_empty CHECK (char_length(segment_id) > 0)
);

CREATE TABLE IF NOT EXISTS outbox_segments (
  device_id TEXT NOT NULL,
  segment_id TEXT NOT NULL,
  head_event_id UUID,
  inflight_event_id UUID,
  updated_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (device_id, segment_id)
);

CREATE INDEX IF NOT EXISTS outbox_ready_idx
  ON outbox (status, send_after)
  WHERE status IN (0, 1);

CREATE INDEX IF NOT EXISTS outbox_device_segment_created_idx
  ON outbox (device_id, segment_id, created_at);

CREATE INDEX IF NOT EXISTS outbox_expires_at_idx
  ON outbox (expires_at);

CREATE INDEX IF NOT EXISTS outbox_device_status_created_idx
  ON outbox (device_id, status, created_at);
