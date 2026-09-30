-- ---- file import (Monarch) ----

-- name: ImportedExternalIDs :many
SELECT external_id FROM transactions
WHERE household_id = ? AND source = 'import' AND external_id IS NOT NULL;

-- Transactions already in an account that an imported row may duplicate (same amount, close
-- date). Imported and provisional rows are never candidates.
-- name: ImportMatchCandidates :many
SELECT id, date, category_id, category_source, notes, needs_review FROM transactions
WHERE account_id = sqlc.arg(account_id) AND amount_cents = sqlc.arg(amount_cents)
  AND date >= sqlc.arg(from_date) AND date <= sqlc.arg(to_date)
  AND source != 'import' AND provisional = 0 AND linked_txn_id IS NULL
ORDER BY date, id;

-- name: InsertImportedTransaction :one
INSERT INTO transactions (
    household_id, account_id, external_id, source, date, amount_cents, description, payee,
    merchant_id, category_id, category_source, notes, needs_review, created_at, updated_at
) VALUES (?, ?, ?, 'import', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id;

-- name: EnrichImportedMatch :exec
UPDATE transactions SET category_id = ?, category_source = ?, notes = ?, needs_review = ?, updated_at = ?
WHERE id = ?;

-- name: InsertBalanceSnapshotIfMissing :exec
INSERT INTO balance_snapshots (account_id, date, balance_cents) VALUES (?, ?, ?)
ON CONFLICT (account_id, date) DO NOTHING;
