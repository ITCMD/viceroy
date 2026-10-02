-- +goose Up
-- An account replaced by another (sync duplicates): it stays as an ignored, hidden tombstone
-- so the bank account it was linked to isn't offered again, and points at its replacement.
ALTER TABLE accounts ADD COLUMN replaced_by INTEGER REFERENCES accounts(id) ON DELETE SET NULL;
-- 1 = the account took over another one's transactions; its next sync lets fetched transactions
-- take over those rows (same amount within 2 days) instead of adding duplicates.
ALTER TABLE accounts ADD COLUMN adopt_rows INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE accounts DROP COLUMN adopt_rows;
ALTER TABLE accounts DROP COLUMN replaced_by;
