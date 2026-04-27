WITH ranked_welcomes AS (
  SELECT
    ctid,
    ROW_NUMBER() OVER (
      PARTITION BY room_id, target_user_public_key
      ORDER BY updated_at DESC, created_at DESC
    ) AS row_number
  FROM chat_room_welcomes
)
DELETE FROM chat_room_welcomes welcomes
USING ranked_welcomes ranked
WHERE welcomes.ctid = ranked.ctid
  AND ranked.row_number > 1;

CREATE UNIQUE INDEX IF NOT EXISTS chat_room_welcomes_pkey
  ON chat_room_welcomes (room_id, target_user_public_key);
