-- Kept so both dialects record the same versions. A PostgreSQL database never
-- held events above event.MaxTokens (ingest rejected them before PostgreSQL
-- was supported), so this deletes nothing.
DELETE FROM usage_events
WHERE input > 10000000000 OR output > 10000000000 OR cache_read > 10000000000
   OR cache_write_5m > 10000000000 OR cache_write_1h > 10000000000
   OR cache_write > 10000000000 OR reasoning > 10000000000;
