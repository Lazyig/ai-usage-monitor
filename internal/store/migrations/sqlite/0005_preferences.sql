-- Dashboard preferences (selected date range, auto refresh), kept on the
-- server so they follow the operator across browsers and devices. One row per
-- named document; value is JSON validated by the API.
CREATE TABLE preferences (
    name       TEXT PRIMARY KEY,
    value      TEXT    NOT NULL,
    updated_at INTEGER NOT NULL
);
