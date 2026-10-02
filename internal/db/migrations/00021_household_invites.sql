-- +goose Up
-- One-time links an admin hands out: a join link adds a new member to the household,
-- a reset link (user_id set) lets an existing member choose a new password. Only a
-- SHA-256 of the token is kept.
CREATE TABLE household_invites (
    id           INTEGER PRIMARY KEY,
    household_id INTEGER NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    user_id      INTEGER REFERENCES users(id) ON DELETE CASCADE, -- NULL = join, else password reset
    label        TEXT    NOT NULL DEFAULT '',
    token_hash   BLOB    NOT NULL UNIQUE,
    created_by   INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at   INTEGER NOT NULL,
    expires_at   INTEGER NOT NULL,
    used_at      INTEGER,
    revoked_at   INTEGER
);
CREATE INDEX household_invites_household ON household_invites(household_id);
ALTER TABLE household_members ADD COLUMN joined_at INTEGER NOT NULL DEFAULT 0;
UPDATE household_members SET joined_at = (SELECT created_at FROM users WHERE users.id = household_members.user_id);

-- +goose Down
ALTER TABLE household_members DROP COLUMN joined_at;
DROP TABLE household_invites;
