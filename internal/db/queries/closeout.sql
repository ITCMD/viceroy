-- name: GetCloseout :one
SELECT * FROM budget_closeouts WHERE household_id = ? AND month = ?;

-- name: ListCloseoutMonths :many
SELECT month FROM budget_closeouts WHERE household_id = ? ORDER BY month DESC;

-- name: CreateCloseout :one
INSERT INTO budget_closeouts (household_id, month, closed_at, closed_by, budget_cents, actual_cents, surplus_cents, review)
VALUES (?, ?, ?, ?, ?, ?, ?, ?) RETURNING *;

-- name: SetCloseoutAnalysis :exec
UPDATE budget_closeouts SET analysis = ?, analysis_at = ? WHERE id = ? AND household_id = ?;

-- name: DeleteCloseout :exec
DELETE FROM budget_closeouts WHERE id = ? AND household_id = ?;

-- name: InsertCloseoutAllocation :exec
INSERT INTO closeout_allocations (closeout_id, household_id, category_id, goal_id, month, amount_cents)
VALUES (?, ?, ?, ?, ?, ?);

-- name: ListCloseoutAllocations :many
SELECT a.id, a.category_id, a.goal_id, a.month, a.amount_cents,
  COALESCE(c.name, g.name, '') AS name, COALESCE(c.icon, g.icon, '') AS icon
FROM closeout_allocations a
LEFT JOIN categories c ON c.id = a.category_id
LEFT JOIN goals g ON g.id = a.goal_id
WHERE a.closeout_id = ? ORDER BY a.id;
