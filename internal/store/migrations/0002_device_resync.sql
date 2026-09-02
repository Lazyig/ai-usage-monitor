-- Device re-sync flag.
--
-- Set when an operator purges a device's events (DELETE /devices/{id}/events).
-- The device keeps its token, but the server no longer holds the history the
-- agent's cursors say it already delivered, so the next ingest answer carries
-- "resync": true exactly once. The agent then drops its cursors and its offline
-- queue and rescans from zero; EventIDs are device independent (scheme 2), so
-- re-sending is idempotent.
ALTER TABLE devices ADD COLUMN resync_requested INTEGER NOT NULL DEFAULT 0;
