-- +goose Up
-- Keys for the REST API (Authorization: Bearer vk_...). Requests act as the user who made
-- the key. Only a SHA-256 of the key is kept; the key itself is shown once.
CREATE TABLE api_keys (
    id           INTEGER PRIMARY KEY,
    household_id INTEGER NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         TEXT    NOT NULL,
    prefix       TEXT    NOT NULL, -- start of the key, to tell keys apart
    token_hash   BLOB    NOT NULL UNIQUE,
    scope        TEXT    NOT NULL, -- read | write
    created_at   INTEGER NOT NULL,
    last_used_at INTEGER,
    revoked_at   INTEGER
);
CREATE INDEX api_keys_household ON api_keys(household_id);

-- +goose Down
DROP TABLE api_keys;
