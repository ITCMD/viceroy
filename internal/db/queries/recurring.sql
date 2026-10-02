-- name: ListRecurringItems :many
SELECT * FROM recurring_items WHERE household_id = ? ORDER BY id;

-- name: GetRecurringItem :one
SELECT * FROM recurring_items WHERE id = ? AND household_id = ?;

-- name: CreateRecurringItem :one
INSERT INTO recurring_items (household_id, name, merchant_id, match_text, account_id, category_id,
    amount_cents, amount_varies, cadence, anchor_date, day2, series_key, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: UpdateRecurringItem :exec
UPDATE recurring_items SET name = ?, merchant_id = ?, match_text = ?, account_id = ?, category_id = ?,
    amount_cents = ?, amount_varies = ?, cadence = ?, anchor_date = ?, day2 = ?, updated_at = ?
WHERE id = ? AND household_id = ?;

-- name: DeleteRecurringItem :execrows
DELETE FROM recurring_items WHERE id = ? AND household_id = ?;

-- name: ListNetworthAnnotations :many
SELECT n.id, n.date, n.label, n.icon, n.transaction_id,
    CAST(COALESCE(m.name, NULLIF(t.payee, ''), t.description, '') AS TEXT) AS txn_name,
    COALESCE(t.amount_cents, 0) AS txn_amount, COALESCE(t.date, '') AS txn_date
FROM networth_annotations n
LEFT JOIN transactions t ON t.id = n.transaction_id
LEFT JOIN merchants m ON m.id = t.merchant_id
WHERE n.household_id = ?
ORDER BY n.date, n.id;

-- name: CreateNetworthAnnotation :one
INSERT INTO networth_annotations (household_id, date, label, icon, transaction_id, created_at)
VALUES (?, ?, ?, ?, ?, ?) RETURNING id;

-- name: UpdateNetworthAnnotation :execrows
UPDATE networth_annotations SET date = ?, label = ?, icon = ?, transaction_id = ?
WHERE id = ? AND household_id = ?;

-- name: DeleteNetworthAnnotation :execrows
DELETE FROM networth_annotations WHERE id = ? AND household_id = ?;
