-- ---- categories ----

-- name: CountCategoryGroups :one
SELECT COUNT(*) FROM category_groups WHERE household_id = ?;

-- name: ListHouseholdIDs :many
SELECT id FROM households;

-- name: CreateCategoryGroup :one
INSERT INTO category_groups (household_id, name, kind, sort) VALUES (?, ?, ?, ?) RETURNING *;

-- name: CreateCategory :one
INSERT INTO categories (household_id, group_id, name, icon, sort) VALUES (?, ?, ?, ?, ?) RETURNING *;

-- name: ListCategoryGroups :many
SELECT * FROM category_groups WHERE household_id = ? ORDER BY sort, id;

-- name: ListCategories :many
SELECT * FROM categories WHERE household_id = ? ORDER BY sort, id;

-- name: GetCategory :one
SELECT * FROM categories WHERE id = ? AND household_id = ?;

-- ---- merchants ----

-- name: UpsertMerchant :one
INSERT INTO merchants (household_id, name, normalized) VALUES (?, ?, ?)
ON CONFLICT (household_id, normalized) DO UPDATE SET normalized = excluded.normalized
RETURNING *;

-- name: GetMerchant :one
SELECT * FROM merchants WHERE id = ? AND household_id = ?;

-- name: RenameMerchant :exec
UPDATE merchants SET name = ? WHERE id = ? AND household_id = ?;

-- Category most recently chosen by the user (or a rule) for a merchant. An AI pick counts too
-- (a "soft rule": the merchant needn't go to the AI again), but anything a person chose wins.
-- name: MerchantHistoryCategory :one
SELECT category_id FROM transactions
WHERE household_id = ? AND merchant_id = ? AND category_id IS NOT NULL
  AND category_source IN ('user', 'rule', 'linked', 'ai')
ORDER BY category_source = 'ai', date DESC, id DESC LIMIT 1;

-- ---- tags ----

-- name: ListTags :many
SELECT * FROM tags WHERE household_id = ? ORDER BY name;

-- name: UpsertTag :one
INSERT INTO tags (household_id, name) VALUES (?, ?)
ON CONFLICT (household_id, name) DO UPDATE SET name = excluded.name
RETURNING *;

-- name: ClearTransactionTags :exec
DELETE FROM transaction_tags WHERE transaction_id = ?;

-- name: AddTransactionTag :exec
INSERT OR IGNORE INTO transaction_tags (transaction_id, tag_id)
SELECT sqlc.arg(transaction_id), t.id FROM tags t WHERE t.id = sqlc.arg(tag_id) AND t.household_id = sqlc.arg(household_id);

-- name: ListTagsForTransactions :many
SELECT tt.transaction_id, t.id, t.name, t.color FROM transaction_tags tt JOIN tags t ON t.id = tt.tag_id
WHERE tt.transaction_id IN (sqlc.slice(ids)) ORDER BY t.name;

-- ---- rules ----

-- name: ListRules :many
SELECT * FROM rules WHERE household_id = ? ORDER BY priority, id;

-- name: GetRule :one
SELECT * FROM rules WHERE id = ? AND household_id = ?;

-- name: CreateRule :one
INSERT INTO rules (
    household_id, priority, match_field, match_op, match_value, account_id, amount_min, amount_max,
    direction, day_min, day_max, set_category_id, set_merchant, set_hidden, set_goal_id, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING *;

-- name: UpdateRule :exec
UPDATE rules SET priority = ?, match_field = ?, match_op = ?, match_value = ?, account_id = ?,
    amount_min = ?, amount_max = ?, direction = ?, day_min = ?, day_max = ?,
    set_category_id = ?, set_merchant = ?, set_hidden = ?, set_goal_id = ?
WHERE id = ? AND household_id = ?;

-- name: ListRuleTags :many
SELECT rt.rule_id, t.id, t.name FROM rule_tags rt JOIN tags t ON t.id = rt.tag_id
JOIN rules r ON r.id = rt.rule_id WHERE r.household_id = ? ORDER BY t.name;

-- name: ClearRuleTags :exec
DELETE FROM rule_tags WHERE rule_id = ?;

-- name: AddRuleTag :exec
INSERT OR IGNORE INTO rule_tags (rule_id, tag_id) VALUES (?, ?);

-- name: ListTransactionTagIDs :many
SELECT tag_id FROM transaction_tags WHERE transaction_id = ?;

-- name: DeleteRule :exec
DELETE FROM rules WHERE id = ? AND household_id = ?;

-- ---- transactions (user side) ----

-- name: ListTransactions :many
SELECT t.*, a.name AS account_name, a.mask AS account_mask,
    COALESCE(m.name, '') AS merchant_name,
    COALESCE(c.name, '') AS category_name, COALESCE(c.icon, '') AS category_icon,
    EXISTS (SELECT 1 FROM transactions p WHERE p.linked_txn_id = t.id) AS has_linked,
    CAST(COALESCE((SELECT p.source FROM transactions p WHERE p.linked_txn_id = t.id ORDER BY p.id LIMIT 1), '') AS TEXT) AS linked_source
FROM transactions t
JOIN accounts a ON a.id = t.account_id
LEFT JOIN merchants m ON m.id = t.merchant_id
LEFT JOIN categories c ON c.id = t.category_id
WHERE t.household_id = sqlc.arg(household_id)
  AND t.linked_txn_id IS NULL
  AND a.status != 'ignored'
  AND (sqlc.narg(account_id) IS NULL OR t.account_id = sqlc.narg(account_id))
  AND (sqlc.narg(category_id) IS NULL OR t.category_id = sqlc.narg(category_id))
  AND (sqlc.arg(uncategorized) = 0 OR t.category_id IS NULL)
  AND (sqlc.arg(needs_review) = 0 OR t.needs_review = 1)
  AND (sqlc.arg(include_hidden) = 1 OR t.hidden = 0)
  AND (sqlc.arg(q) = '' OR t.description LIKE '%' || sqlc.arg(q) || '%' OR m.name LIKE '%' || sqlc.arg(q) || '%'
       OR t.notes LIKE '%' || sqlc.arg(q) || '%' OR t.payee LIKE '%' || sqlc.arg(q) || '%')
  AND (sqlc.arg(before_date) = '' OR t.date < sqlc.arg(before_date)
       OR (t.date = sqlc.arg(before_date) AND t.id < sqlc.arg(before_id)))
ORDER BY t.date DESC, t.id DESC
LIMIT sqlc.arg(lim);

-- name: GetTransactionView :one
SELECT t.*, a.name AS account_name, a.mask AS account_mask,
    COALESCE(m.name, '') AS merchant_name,
    COALESCE(c.name, '') AS category_name, COALESCE(c.icon, '') AS category_icon,
    EXISTS (SELECT 1 FROM transactions p WHERE p.linked_txn_id = t.id) AS has_linked,
    CAST(COALESCE((SELECT p.source FROM transactions p WHERE p.linked_txn_id = t.id ORDER BY p.id LIMIT 1), '') AS TEXT) AS linked_source
FROM transactions t
JOIN accounts a ON a.id = t.account_id
LEFT JOIN merchants m ON m.id = t.merchant_id
LEFT JOIN categories c ON c.id = t.category_id
WHERE t.id = ? AND t.household_id = ?;

-- name: GetTransaction :one
SELECT * FROM transactions WHERE id = ? AND household_id = ?;

-- name: ListMerchantTransactions :many
SELECT t.id, t.date, t.amount_cents, t.description, a.name AS account_name, COALESCE(c.name, '') AS category_name
FROM transactions t
JOIN accounts a ON a.id = t.account_id
LEFT JOIN categories c ON c.id = t.category_id
WHERE t.household_id = ? AND t.merchant_id = ? AND t.id != ? AND t.linked_txn_id IS NULL
ORDER BY t.date DESC, t.id DESC LIMIT 50;

-- name: InsertManualTransaction :one
INSERT INTO transactions (
    household_id, account_id, source, date, amount_cents, description, payee, pending, provisional,
    merchant_id, category_id, category_source, notes, needs_review, owner_user_id, created_at, updated_at
) VALUES (?, ?, 'manual', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id;

-- name: UpdateTransactionUser :exec
UPDATE transactions SET
    date = ?, amount_cents = ?, description = ?, merchant_id = ?, category_id = ?, category_source = ?,
    notes = ?, hidden = ?, needs_review = ?, updated_at = ?
WHERE id = ? AND household_id = ?;

-- name: SetTransactionAuto :exec
UPDATE transactions SET merchant_id = ?, category_id = ?, category_source = ?, needs_review = ?, hidden = ?
WHERE id = ?;

-- name: DeleteManualTransaction :exec
DELETE FROM transactions WHERE id = ? AND household_id = ? AND source = 'manual';

-- Transactions a rule may apply to (it never overrides a category the user chose by hand).
-- name: ListRuleTargets :many
SELECT t.id, t.account_id, t.date, t.amount_cents, t.description, t.payee, t.merchant_id, COALESCE(m.name, '') AS merchant_name
FROM transactions t LEFT JOIN merchants m ON m.id = t.merchant_id
WHERE t.household_id = ? AND t.linked_txn_id IS NULL;

-- ---- linking ----

-- name: ListLinkCandidates :many
SELECT t.id, t.date, t.amount_cents, t.description, t.payee, COALESCE(m.name, '') AS merchant_name
FROM transactions t LEFT JOIN merchants m ON m.id = t.merchant_id
WHERE t.account_id = sqlc.arg(account_id) AND t.provisional = 1 AND t.linked_txn_id IS NULL
  AND t.amount_cents >= sqlc.arg(amount_lo) AND t.amount_cents <= sqlc.arg(amount_hi)
  AND t.date >= sqlc.arg(date_lo) AND t.date <= sqlc.arg(date_hi)
  AND NOT EXISTS (SELECT 1 FROM link_blacklist b WHERE b.provisional_id = t.id AND b.posted_id = sqlc.arg(posted_id));

-- Posted rows a provisional entry could be linked to by hand.
-- name: ListPostedForLink :many
SELECT t.id, t.date, t.amount_cents, t.description, COALESCE(m.name, '') AS merchant_name,
    CAST(ABS(t.amount_cents - sqlc.arg(amount_cents)) AS INTEGER) AS distance
FROM transactions t LEFT JOIN merchants m ON m.id = t.merchant_id
WHERE t.account_id = sqlc.arg(account_id) AND t.provisional = 0
  AND t.date >= sqlc.arg(date_lo) AND t.date <= sqlc.arg(date_hi)
  AND NOT EXISTS (SELECT 1 FROM transactions p WHERE p.linked_txn_id = t.id)
ORDER BY distance, t.date DESC
LIMIT 20;

-- Posted rows that look like the same purchase as a new pending entry (duplicate warning).
-- name: ListPostedDuplicates :many
SELECT t.id, t.date, t.amount_cents, t.description, COALESCE(m.name, '') AS merchant_name
FROM transactions t LEFT JOIN merchants m ON m.id = t.merchant_id
WHERE t.account_id = ? AND t.provisional = 0 AND t.amount_cents = ? AND t.date >= sqlc.arg(date_lo) AND t.date <= sqlc.arg(date_hi);

-- name: LinkTransaction :exec
UPDATE transactions SET linked_txn_id = ?, linked_at = ? WHERE id = ?;

-- name: UnlinkTransaction :exec
UPDATE transactions SET linked_txn_id = NULL, linked_at = NULL WHERE id = ?;

-- name: BlacklistLink :exec
INSERT OR IGNORE INTO link_blacklist (provisional_id, posted_id) VALUES (?, ?);

-- name: ListLinkedProvisional :many
SELECT * FROM transactions WHERE linked_txn_id = ? ORDER BY id;

-- name: CopyTransactionTags :exec
INSERT OR IGNORE INTO transaction_tags (transaction_id, tag_id)
SELECT sqlc.arg(to_id), src.tag_id FROM transaction_tags src WHERE src.transaction_id = sqlc.arg(from_id);

-- name: GetTransactionByID :one
SELECT * FROM transactions WHERE id = ?;

-- ---- AI categorization ----

-- Uncategorized money movements the AI may look at. `tried` rows (the AI already passed on
-- them) are skipped unless include_tried.
-- name: ListUncategorizedForAI :many
SELECT t.id, t.merchant_id, t.amount_cents, t.description, t.payee, COALESCE(m.name, '') AS merchant_name
FROM transactions t
JOIN accounts a ON a.id = t.account_id
LEFT JOIN merchants m ON m.id = t.merchant_id
WHERE t.household_id = sqlc.arg(household_id) AND t.category_id IS NULL AND t.hidden = 0
  AND t.linked_txn_id IS NULL AND a.status != 'ignored'
  AND t.date >= sqlc.arg(since) AND (sqlc.arg(include_tried) = 1 OR t.ai_cat_tried = 0)
  AND t.created_at >= sqlc.arg(created_since)
ORDER BY t.date DESC, t.id DESC LIMIT 2000;

-- name: SetTransactionAICategory :exec
UPDATE transactions SET category_id = ?, category_source = 'ai', needs_review = ?, ai_cat_tried = 1
WHERE id = ? AND household_id = ? AND category_id IS NULL;

-- name: MarkAICategoryTried :exec
UPDATE transactions SET ai_cat_tried = 1 WHERE id = ? AND household_id = ?;

-- name: SetCategoryPlace :exec
UPDATE categories SET group_id = ?, sort = ? WHERE id = ? AND household_id = ?;
