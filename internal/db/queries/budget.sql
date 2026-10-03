-- ---- household settings ----

-- name: ListHouseholdSettings :many
SELECT key, value FROM household_settings WHERE household_id = ?;

-- name: SetHouseholdSetting :exec
INSERT INTO household_settings (household_id, key, value) VALUES (?, ?, ?)
ON CONFLICT (household_id, key) DO UPDATE SET value = excluded.value;

-- ---- budget amounts ----

-- name: ListBudgetAmounts :many
SELECT * FROM budget_amounts WHERE household_id = ? ORDER BY month;

-- All rows of one category, goal or debt account are rewritten together (see budget.SetAmount).
-- name: DeleteBudgetAmountsFor :exec
DELETE FROM budget_amounts
WHERE household_id = sqlc.arg(household_id)
  AND category_id IS sqlc.narg(category_id) AND goal_id IS sqlc.narg(goal_id) AND account_id IS sqlc.narg(account_id);

-- name: InsertBudgetAmount :exec
INSERT INTO budget_amounts (household_id, category_id, goal_id, account_id, month, amount_cents, forward)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: GetBuiltinCategory :one
SELECT * FROM categories WHERE household_id = ? AND builtin = ?;

-- name: SetCategoryBuiltin :exec
UPDATE categories SET builtin = ? WHERE id = ? AND household_id = ?;

-- name: SetCategoryChunk :exec
UPDATE categories SET chunk = ? WHERE id = ? AND household_id = ?;

-- ---- totals ----

-- Signed daily totals per category over [from, to), for budgets and history. Leaves out
-- hidden rows, linked provisional rows (their posted twin counts) and ignored accounts.
-- name: DailyCategoryTotals :many
SELECT t.category_id AS category_id, t.date, CAST(SUM(t.amount_cents) AS INTEGER) AS total
FROM transactions t JOIN accounts a ON a.id = t.account_id
WHERE t.household_id = sqlc.arg(household_id) AND t.date >= sqlc.arg(from_date) AND t.date < sqlc.arg(to_date)
  AND t.category_id IS NOT NULL AND t.hidden = 0 AND t.linked_txn_id IS NULL AND a.status != 'ignored'
GROUP BY t.category_id, t.date;

-- Money put toward each goal per day (absolute amounts) over [from, to). Money spent from a
-- goal (goal_withdrawal) isn't a contribution.
-- name: DailyGoalTotals :many
SELECT t.goal_id AS goal_id, t.date, CAST(SUM(ABS(t.amount_cents)) AS INTEGER) AS total
FROM transactions t JOIN accounts a ON a.id = t.account_id
WHERE t.household_id = sqlc.arg(household_id) AND t.date >= sqlc.arg(from_date) AND t.date < sqlc.arg(to_date)
  AND t.goal_id IS NOT NULL AND t.goal_withdrawal = 0 AND t.hidden = 0 AND t.linked_txn_id IS NULL AND a.status != 'ignored'
GROUP BY t.goal_id, t.date;

-- ---- goals ----

-- name: ListGoals :many
SELECT g.*, CAST(COALESCE((
    SELECT SUM(ABS(t.amount_cents)) FROM transactions t
    WHERE t.goal_id = g.id AND t.goal_withdrawal = 0 AND t.hidden = 0 AND t.linked_txn_id IS NULL), 0) AS INTEGER) AS contributed_cents,
  CAST(COALESCE((
    SELECT SUM(ABS(t.amount_cents)) FROM transactions t
    WHERE t.goal_id = g.id AND t.goal_withdrawal = 1 AND t.hidden = 0 AND t.linked_txn_id IS NULL), 0) AS INTEGER) AS withdrawn_cents
FROM goals g WHERE g.household_id = ? ORDER BY g.archived, g.sort, g.id;

-- name: GetGoal :one
SELECT * FROM goals WHERE id = ? AND household_id = ?;

-- name: CreateGoal :one
INSERT INTO goals (household_id, name, icon, target_cents, target_date, starting_cents, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING *;

-- name: UpdateGoal :exec
UPDATE goals SET name = ?, icon = ?, target_cents = ?, target_date = ?, starting_cents = ?, archived = ?
WHERE id = ? AND household_id = ?;

-- name: ClearGoalTransactions :exec
UPDATE transactions SET goal_id = NULL WHERE goal_id = ? AND household_id = ?;

-- name: DeleteGoal :exec
DELETE FROM goals WHERE id = ? AND household_id = ?;

-- Moving a transaction to another goal (or none) makes it a contribution again.
-- name: SetTransactionGoal :exec
UPDATE transactions SET goal_withdrawal = CASE WHEN goal_id IS sqlc.narg(goal_id) THEN goal_withdrawal ELSE 0 END,
    goal_id = sqlc.narg(goal_id)
WHERE id = sqlc.arg(id) AND household_id = sqlc.arg(household_id);

-- name: SetTransactionGoalWithdrawal :exec
UPDATE transactions SET goal_id = ?, goal_withdrawal = ? WHERE id = ? AND household_id = ?;

-- name: GetBuiltinGoal :one
SELECT * FROM goals WHERE household_id = ? AND builtin = ? ORDER BY id LIMIT 1;

-- name: CreateBuiltinGoal :one
INSERT INTO goals (household_id, name, icon, builtin, created_at) VALUES (?, ?, ?, ?, ?) RETURNING *;

-- name: GetCategoryGroupKind :one
SELECT kind FROM category_groups WHERE id = ?;

-- name: SetCategoryBudgetHidden :exec
UPDATE categories SET budget_hidden = ? WHERE id = ? AND household_id = ?;

-- ---- debt repayment ----

-- Money in (payments, refunds) and out (charges, interest) per day on debt accounts over
-- [from, to), with the same exclusions as DailyCategoryTotals.
-- name: DailyDebtAccountFlows :many
SELECT t.account_id, t.date,
  CAST(SUM(CASE WHEN t.amount_cents > 0 THEN t.amount_cents ELSE 0 END) AS INTEGER) AS paid,
  CAST(SUM(CASE WHEN t.amount_cents < 0 THEN -t.amount_cents ELSE 0 END) AS INTEGER) AS charged
FROM transactions t JOIN accounts a ON a.id = t.account_id
WHERE t.household_id = sqlc.arg(household_id) AND t.date >= sqlc.arg(from_date) AND t.date < sqlc.arg(to_date)
  AND t.hidden = 0 AND t.linked_txn_id IS NULL AND a.status != 'ignored'
  AND a.type IN ('credit_card', 'loan', 'mortgage', 'other_liability')
GROUP BY t.account_id, t.date;

-- Single payments into debt accounts over [from, to): the other side of a payment made from
-- a bank account, used to tell which debt a Debt Repayment transaction paid.
-- name: DebtAccountPayments :many
SELECT t.account_id, t.date, t.amount_cents
FROM transactions t JOIN accounts a ON a.id = t.account_id
WHERE t.household_id = sqlc.arg(household_id) AND t.date >= sqlc.arg(from_date) AND t.date < sqlc.arg(to_date)
  AND t.amount_cents > 0 AND t.hidden = 0 AND t.linked_txn_id IS NULL AND a.status != 'ignored'
  AND a.type IN ('credit_card', 'loan', 'mortgage', 'other_liability')
ORDER BY t.date, t.id;

-- Single transactions in one category over [from, to), with the account they're on.
-- name: CategoryTxns :many
SELECT t.id, t.account_id, t.date, t.amount_cents
FROM transactions t JOIN accounts a ON a.id = t.account_id
WHERE t.household_id = sqlc.arg(household_id) AND t.category_id = sqlc.arg(category_id)
  AND t.date >= sqlc.arg(from_date) AND t.date < sqlc.arg(to_date)
  AND t.hidden = 0 AND t.linked_txn_id IS NULL AND a.status != 'ignored'
ORDER BY t.date, t.id;
