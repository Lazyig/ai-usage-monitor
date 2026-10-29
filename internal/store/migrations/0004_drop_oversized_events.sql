-- Events with a token count above event.MaxTokens (1e10) are rejected on
-- ingest since that cap was added. One stored before it can overflow SQLite's
-- integer SUM and fail every summary that covers it, so it goes too.
DELETE FROM usage_events
WHERE input > 10000000000 OR output > 10000000000 OR cache_read > 10000000000
   OR cache_write_5m > 10000000000 OR cache_write_1h > 10000000000
   OR cache_write > 10000000000 OR reasoning > 10000000000;
