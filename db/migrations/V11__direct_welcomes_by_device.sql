ALTER TABLE chat_room_welcomes
  ADD COLUMN IF NOT EXISTS target_device_id TEXT;

UPDATE chat_room_welcomes
  SET target_device_id = ''
  WHERE target_device_id IS NULL;

ALTER TABLE chat_room_welcomes
  ALTER COLUMN target_device_id SET NOT NULL;

DO $$
BEGIN
  -- Recreate the primary key to include device_id. Some environments may have
  -- a differently named constraint; guard with exception handling.
  BEGIN
    ALTER TABLE chat_room_welcomes DROP CONSTRAINT chat_room_welcomes_pkey;
  EXCEPTION WHEN undefined_object THEN
    -- ignore
  END;
END $$;

ALTER TABLE chat_room_welcomes
  ADD PRIMARY KEY (room_id, target_user_public_key, target_device_id);

