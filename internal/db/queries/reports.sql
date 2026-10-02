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

-- Totals per category, merchant and goal over [from, to) for the spending tree and cash flow
-- diagram. Same exclusions as ReportRows. Rows put toward a goal (not withdrawals) carry
-- goal_id and their absolute amount, like goal contributions in the budget.
-- name: ReportTreeRows :many
SELECT t.category_id, COALESCE(g.kind, '') AS kind,
    CAST(COALESCE(m.name, NULLIF(t.payee, ''), t.description) AS TEXT) AS merchant,
    CAST(CASE WHEN t.goal_withdrawal = 0 THEN COALESCE(t.goal_id, 0) ELSE 0 END AS INTEGER) AS goal_id,
    CAST(SUM(CASE WHEN t.goal_id IS NOT NULL AND t.goal_withdrawal = 0 THEN ABS(t.amount_cents) ELSE t.amount_cents END) AS INTEGER) AS total,
    CAST(COUNT(*) AS INTEGER) AS n
FROM transactions t
JOIN accounts a ON a.id = t.account_id
LEFT JOIN merchants m ON m.id = t.merchant_id
LEFT JOIN categories c ON c.id = t.category_id
LEFT JOIN category_groups g ON g.id = c.group_id
WHERE t.household_id = sqlc.arg(household_id) AND t.date >= sqlc.arg(from_date) AND t.date < sqlc.arg(to_date)
  AND t.hidden = 0 AND t.linked_txn_id IS NULL AND a.status != 'ignored'
GROUP BY t.category_id, 3, 4;

-- Interest and finance charges posted to debt accounts since a date, per account.
-- name: InterestCharges :many
SELECT t.account_id, CAST(SUM(-t.amount_cents) AS INTEGER) AS total
FROM transactions t
JOIN accounts a ON a.id = t.account_id
WHERE t.household_id = sqlc.arg(household_id) AND t.date >= sqlc.arg(from_date) AND t.amount_cents < 0
  AND t.hidden = 0 AND t.linked_txn_id IS NULL
  AND a.type IN ('credit_card', 'loan', 'mortgage', 'other_liability')
  AND (LOWER(t.description) LIKE '%interest%' OR LOWER(t.description) LIKE '%finance charge%')
GROUP BY t.account_id;

-- name: FirstTransactionDate :one
SELECT CAST(COALESCE(MIN(date), '') AS TEXT) FROM transactions WHERE household_id = ? AND hidden = 0;

-- ---- recurring ----

-- History the recurring detector reads: non-transfer, visible, unlinked rows since a date.
-- name: RecurringCandidates :many
SELECT t.id, t.date, t.amount_cents, t.merchant_id, t.description,
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
