-- Initial schema.
--
-- Timestamps are unix milliseconds (INTEGER) so range scans and bucketing are
-- plain integer comparisons; wall-clock/timezone work happens in Go.
--
-- No daily rollup table in v1: SQLite aggregates a few hundred thousand rows in
-- single-digit milliseconds, and every query in query.go groups by
-- (dimension, source, model, bucket). A rollup can be added later as migration
-- 0002 (same grouping, pre-summed) without changing any query shape.

CREATE TABLE devices (
    id               TEXT PRIMARY KEY,
    name             TEXT    NOT NULL DEFAULT '',
    os               TEXT    NOT NULL DEFAULT '',
    arch             TEXT    NOT NULL DEFAULT '',
    agent_version    TEXT    NOT NULL DEFAULT '',
    token_hash       TEXT    NOT NULL UNIQUE, -- sha256 hex of the presented token; the token itself is never stored
    token_created_at INTEGER NOT NULL,
    last_seen        INTEGER,
    last_ingest      INTEGER,
    revoked_at       INTEGER
);

CREATE TABLE usage_events (
    event_id       TEXT PRIMARY KEY,
    device_id      TEXT    NOT NULL REFERENCES devices(id),
    source         TEXT    NOT NULL,
    ts             INTEGER NOT NULL, -- unix milliseconds, UTC
    model          TEXT    NOT NULL DEFAULT '',
    session_id     TEXT    NOT NULL DEFAULT '',
    project        TEXT    NOT NULL DEFAULT '',
    input          INTEGER NOT NULL DEFAULT 0,
    output         INTEGER NOT NULL DEFAULT 0,
    cache_read     INTEGER NOT NULL DEFAULT 0,
    cache_write_5m INTEGER NOT NULL DEFAULT 0,
    cache_write_1h INTEGER NOT NULL DEFAULT 0,
    cache_write    INTEGER NOT NULL DEFAULT 0,
    reasoning      INTEGER NOT NULL DEFAULT 0,
    confidence     TEXT    NOT NULL DEFAULT '',
    tool_version   TEXT    NOT NULL DEFAULT '',
    received_at    INTEGER NOT NULL
);

CREATE INDEX idx_usage_events_ts ON usage_events(ts);
CREATE INDEX idx_usage_events_device_ts ON usage_events(device_id, ts);
CREATE INDEX idx_usage_events_source_model_ts ON usage_events(source, model, ts);
