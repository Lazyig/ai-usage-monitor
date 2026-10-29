-- Device re-sync flag. See ../sqlite/0002_device_resync.sql.
ALTER TABLE devices ADD COLUMN resync_requested BIGINT NOT NULL DEFAULT 0;
