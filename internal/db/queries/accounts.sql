-- ---- connections ----

-- name: CreateConnection :one
INSERT INTO connections (household_id, provider, name, secret_enc, created_at, next_sync_at)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetConnection :one
SELECT * FROM connections WHERE id = ? AND household_id = ?;

-- name: GetConnectionByID :one
SELECT * FROM connections WHERE id = ?;

-- name: ListConnections :many
SELECT * FROM connections WHERE household_id = ? ORDER BY id;

-- name: ListDueConnections :many
SELECT * FROM connections
WHERE status != 'revoked' AND next_sync_at IS NOT NULL AND next_sync_at <= ?
ORDER BY next_sync_at;

-- name: SetConnectionRequests :exec
UPDATE connections SET requests_day = ?, requests_count = ? WHERE id = ?;

-- name: SetConnectionSyncOK :exec
UPDATE connections
SET status = 'active', last_error = '', last_sync_at = ?, synced_through = ?, next_sync_at = ?
WHERE id = ?;

-- name: SetConnectionSyncError :exec
UPDATE connections SET status = ?, last_error = ?, next_sync_at = ? WHERE id = ?;

-- name: DeleteConnection :exec
DELETE FROM connections WHERE id = ? AND household_id = ?;

-- ---- institutions ----

-- name: UpsertInstitution :one
INSERT INTO institutions (connection_id, external_id, name, url)
VALUES (?, ?, ?, ?)
ON CONFLICT (connection_id, external_id) DO UPDATE SET name = excluded.name, url = excluded.url
RETURNING *;

-- name: SetInstitutionStatus :exec
UPDATE institutions SET status = ?, last_error = ? WHERE id = ?;

-- name: ListInstitutions :many
SELECT i.* FROM institutions i
JOIN connections c ON c.id = i.connection_id
WHERE c.household_id = ?
ORDER BY i.id;

-- ---- accounts ----

-- name: ListAccounts :many
SELECT * FROM accounts WHERE household_id = ? ORDER BY type, name, id;

-- name: GetAccount :one
SELECT * FROM accounts WHERE id = ? AND household_id = ?;

-- name: GetAccountByExternal :one
SELECT * FROM accounts WHERE connection_id = ? AND external_id = ?;

-- name: ListConnectionAccounts :many
SELECT * FROM accounts WHERE connection_id = ?;

-- name: CreateAccount :one
INSERT INTO accounts (
    household_id, connection_id, institution_id, external_id, institution_name, provider_name,
    name, mask, type, currency, balance_cents, available_cents, balance_at, status,
    review_candidate_id, is_manual, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: UpdateAccountFromSync :exec
UPDATE accounts
SET institution_id = ?, institution_name = ?, provider_name = ?, currency = ?,
    balance_cents = ?, available_cents = ?, balance_at = ?,
    status = CASE WHEN status = 'disconnected' THEN 'active' ELSE status END,
    updated_at = ?
WHERE id = ?;

-- name: RelinkAccount :exec
UPDATE accounts
SET connection_id = ?, institution_id = ?, external_id = ?, institution_name = ?, provider_name = ?,
    status = 'active', review_candidate_id = NULL, updated_at = ?
WHERE id = ?;

-- name: ClearAccountExternal :exec
UPDATE accounts SET external_id = NULL, updated_at = ? WHERE id = ?;

-- name: SetAccountStatus :exec
UPDATE accounts SET status = ?, review_candidate_id = NULL, updated_at = ? WHERE id = ?;

-- name: UpdateAccountSettings :exec
UPDATE accounts
SET name = ?, type = ?, include_in_net_worth = ?, hidden = ?, status = ?, updated_at = ?
WHERE id = ? AND household_id = ?;

-- name: SetManualBalance :exec
UPDATE accounts SET balance_cents = ?, balance_at = ?, updated_at = ? WHERE id = ? AND household_id = ?;

-- name: DeleteAccount :exec
DELETE FROM accounts WHERE id = ? AND household_id = ?;

-- ---- balances ----

-- name: UpsertBalanceSnapshot :exec
INSERT INTO balance_snapshots (account_id, date, balance_cents) VALUES (?, ?, ?)
ON CONFLICT (account_id, date) DO UPDATE SET balance_cents = excluded.balance_cents;

-- name: ListHouseholdSnapshots :many
SELECT s.account_id, s.date, s.balance_cents FROM balance_snapshots s
JOIN accounts a ON a.id = s.account_id
WHERE a.household_id = ? AND s.date >= ?
ORDER BY s.date;

-- name: ListAccountSnapshotsBefore :many
-- Latest snapshot per account strictly before a date, used to seed history.
SELECT s.account_id, s.date, s.balance_cents FROM balance_snapshots s
JOIN accounts a ON a.id = s.account_id
WHERE a.household_id = ? AND s.date = (
    SELECT MAX(s2.date) FROM balance_snapshots s2 WHERE s2.account_id = s.account_id AND s2.date < ?
);

-- name: MoveSnapshots :exec
INSERT OR IGNORE INTO balance_snapshots (account_id, date, balance_cents)
SELECT sqlc.arg(into_id), src.date, src.balance_cents FROM balance_snapshots src WHERE src.account_id = sqlc.arg(from_id);

-- ---- transactions (sync side) ----

-- name: GetTransactionByExternal :one
SELECT id, pending FROM transactions WHERE account_id = ? AND external_id = ?;

-- name: InsertSyncedTransaction :one
INSERT INTO transactions (
    household_id, account_id, external_id, source, date, amount_cents, description, payee, memo,
    pending, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id;

-- name: UpdateSyncedTransaction :exec
UPDATE transactions
SET date = ?, amount_cents = ?, description = ?, payee = ?, memo = ?, pending = ?, updated_at = ?
WHERE id = ?;

-- name: ListPendingSynced :many
SELECT id, external_id FROM transactions
WHERE account_id = ? AND source = ? AND pending = 1 AND date >= ?;

-- name: DeleteTransaction :exec
DELETE FROM transactions WHERE id = ?;

-- name: ListAccountTransactionKeys :many
SELECT id, external_id, date, amount_cents, pending FROM transactions WHERE account_id = ?;

-- name: MoveTransaction :exec
UPDATE transactions SET account_id = ?, updated_at = ? WHERE id = ?;

-- name: CountAccountTransactions :one
SELECT COUNT(*) FROM transactions WHERE account_id = ?;

-- ---- sync events ----

-- name: InsertSyncEvent :exec
INSERT INTO sync_events (connection_id, at, kind, account_id, message) VALUES (?, ?, ?, ?, ?);

-- name: ListSyncEvents :many
SELECT * FROM sync_events WHERE connection_id = ? ORDER BY at DESC, id DESC LIMIT ?;

-- name: SetTransactionExternal :exec
UPDATE transactions SET external_id = ? WHERE id = ?;

-- name: DisconnectConnectionAccounts :exec
UPDATE accounts SET status = 'disconnected', updated_at = ?
WHERE connection_id = ? AND status IN ('active', 'review');

-- ---- built-in accounts ----

-- name: GetBuiltinAccount :one
SELECT * FROM accounts WHERE household_id = ? AND builtin = ?;

-- name: CreateBuiltinAccount :one
INSERT INTO accounts (household_id, name, type, currency, balance_cents, balance_at, status, is_manual, builtin, created_at, updated_at)
VALUES (?, ?, ?, 'USD', 0, ?, 'active', 1, ?, ?, ?)
RETURNING *;

-- Manual accounts only: a manual transaction moved money in or out.
-- name: AdjustManualBalance :one
UPDATE accounts SET balance_cents = balance_cents + sqlc.arg(delta), balance_at = sqlc.arg(now), updated_at = sqlc.arg(now)
WHERE id = sqlc.arg(id) AND is_manual = 1
RETURNING balance_cents;

-- Per-account daily sums of settled transactions, used to rebuild balances before the first
-- snapshot. Provisional rows (manual pending entries, email alerts on synced accounts) are left
-- out: the bank balance doesn't include them yet.
-- name: DailyAccountTotals :many
SELECT t.account_id, t.date, CAST(SUM(t.amount_cents) AS INTEGER) AS total
FROM transactions t
WHERE t.household_id = ? AND t.provisional = 0
GROUP BY t.account_id, t.date;
