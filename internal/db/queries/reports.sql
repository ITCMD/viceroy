-- ---- reports ----

-- Signed totals per day, category and merchant over [from, to). Same exclusions as the
-- budget totals: hidden rows, linked provisional rows and ignored accounts.
-- name: ReportRows :many
SELECT t.date, t.category_id, COALESCE(g.kind, '') AS kind,
    CAST(COALESCE(m.name, NULLIF(t.payee, ''), t.description) AS TEXT) AS merchant,
    CAST(SUM(t.amount_cents) AS INTEGER) AS total
FROM transactions t
JOIN accounts a ON a.id = t.account_id
LEFT JOIN merchants m ON m.id = t.merchant_id
LEFT JOIN categories c ON c.id = t.category_id
LEFT JOIN category_groups g ON g.id = c.group_id
WHERE t.household_id = sqlc.arg(household_id) AND t.date >= sqlc.arg(from_date) AND t.date < sqlc.arg(to_date)
  AND t.hidden = 0 AND t.linked_txn_id IS NULL AND a.status != 'ignored'
GROUP BY t.date, t.category_id, merchant;

-- name: FirstTransactionDate :one
SELECT CAST(COALESCE(MIN(date), '') AS TEXT) FROM transactions WHERE household_id = ? AND hidden = 0;

-- ---- recurring ----

-- History the recurring detector reads: non-transfer, visible, unlinked rows since a date.
-- name: RecurringCandidates :many
SELECT t.id, t.date, t.amount_cents, t.merchant_id,
    CAST(COALESCE(m.name, NULLIF(t.payee, ''), t.description) AS TEXT) AS merchant,
    t.category_id, COALESCE(c.name, '') AS category_name, COALESCE(c.icon, '') AS category_icon,
    t.account_id, a.name AS account_name
FROM transactions t
JOIN accounts a ON a.id = t.account_id
LEFT JOIN merchants m ON m.id = t.merchant_id
LEFT JOIN categories c ON c.id = t.category_id
LEFT JOIN category_groups g ON g.id = c.group_id
WHERE t.household_id = sqlc.arg(household_id) AND t.date >= sqlc.arg(from_date)
  AND t.hidden = 0 AND t.linked_txn_id IS NULL AND a.status != 'ignored'
  AND COALESCE(g.kind, '') != 'transfer'
ORDER BY t.date, t.id;

-- name: ListRecurringDismissed :many
SELECT key FROM recurring_dismissed WHERE household_id = ?;

-- name: DismissRecurring :exec
INSERT OR IGNORE INTO recurring_dismissed (household_id, key) VALUES (?, ?);

-- name: RestoreRecurring :exec
DELETE FROM recurring_dismissed WHERE household_id = ? AND key = ?;
