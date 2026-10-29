-- Dashboard preferences. See ../sqlite/0005_preferences.sql.
CREATE TABLE preferences (
    name       TEXT COLLATE "C" PRIMARY KEY,
    value      TEXT COLLATE "C" NOT NULL,
    updated_at BIGINT NOT NULL
);
