-- Initial schema (PostgreSQL). Same tables, columns and indexes as
-- ../sqlite/0001_init.sql; every migration exists once per dialect under the
-- same file name.
--
-- Integers are BIGINT (timestamps are unix milliseconds, token counts reach
-- 1e10). Text is COLLATE "C": ordering and keyset paging compare bytes, exactly
-- like SQLite, whatever locale the database was created with.

CREATE TABLE devices (
    id               TEXT COLLATE "C" PRIMARY KEY,
    name             TEXT COLLATE "C" NOT NULL DEFAULT '',
    os               TEXT COLLATE "C" NOT NULL DEFAULT '',
    arch             TEXT COLLATE "C" NOT NULL DEFAULT '',
    agent_version    TEXT COLLATE "C" NOT NULL DEFAULT '',
    token_hash       TEXT COLLATE "C" NOT NULL UNIQUE, -- sha256 hex of the presented token; the token itself is never stored
    token_created_at BIGINT NOT NULL,
    last_seen        BIGINT,
    last_ingest      BIGINT,
    revoked_at       BIGINT
);

CREATE TABLE usage_events (
    event_id       TEXT COLLATE "C" PRIMARY KEY,
    device_id      TEXT COLLATE "C" NOT NULL REFERENCES devices(id),
    source         TEXT COLLATE "C" NOT NULL,
    ts             BIGINT NOT NULL, -- unix milliseconds, UTC
    model          TEXT COLLATE "C" NOT NULL DEFAULT '',
    session_id     TEXT COLLATE "C" NOT NULL DEFAULT '',
    project        TEXT COLLATE "C" NOT NULL DEFAULT '',
    input          BIGINT NOT NULL DEFAULT 0,
    output         BIGINT NOT NULL DEFAULT 0,
    cache_read     BIGINT NOT NULL DEFAULT 0,
    cache_write_5m BIGINT NOT NULL DEFAULT 0,
    cache_write_1h BIGINT NOT NULL DEFAULT 0,
    cache_write    BIGINT NOT NULL DEFAULT 0,
    reasoning      BIGINT NOT NULL DEFAULT 0,
    confidence     TEXT COLLATE "C" NOT NULL DEFAULT '',
    tool_version   TEXT COLLATE "C" NOT NULL DEFAULT '',
    received_at    BIGINT NOT NULL
);

CREATE INDEX idx_usage_events_ts ON usage_events(ts);
CREATE INDEX idx_usage_events_device_ts ON usage_events(device_id, ts);
CREATE INDEX idx_usage_events_source_model_ts ON usage_events(source, model, ts);
