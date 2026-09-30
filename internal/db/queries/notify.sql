-- ---- push subscriptions ----

-- name: UpsertPushSubscription :one
INSERT INTO push_subscriptions (user_id, endpoint, p256dh, auth, user_agent, created_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (endpoint) DO UPDATE SET user_id = excluded.user_id, p256dh = excluded.p256dh,
    auth = excluded.auth, user_agent = excluded.user_agent
RETURNING *;

-- name: ListPushSubscriptions :many
SELECT * FROM push_subscriptions WHERE user_id = ? ORDER BY id;

-- name: DeletePushSubscription :exec
DELETE FROM push_subscriptions WHERE id = ? AND user_id = ?;

-- name: DeletePushEndpoint :exec
DELETE FROM push_subscriptions WHERE endpoint = ?;

-- ---- prefs ----

-- name: GetNotificationPrefs :one
SELECT prefs FROM notification_prefs WHERE user_id = ?;

-- name: SetNotificationPrefs :exec
INSERT INTO notification_prefs (user_id, prefs) VALUES (?, ?)
ON CONFLICT (user_id) DO UPDATE SET prefs = excluded.prefs;

-- name: ListHouseholdUsers :many
SELECT u.* FROM users u JOIN household_members m ON m.user_id = u.id
WHERE m.household_id = ? ORDER BY u.id;

-- ---- notifications ----

-- Returns no row when the dedupe key was already used.
-- name: InsertNotification :one
INSERT INTO notifications (user_id, kind, dedupe_key, title, body, url, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (user_id, dedupe_key) DO NOTHING
RETURNING *;

-- name: ListNotifications :many
SELECT * FROM notifications WHERE user_id = ? ORDER BY created_at DESC, id DESC LIMIT ?;

-- name: CountUnreadNotifications :one
SELECT COUNT(*) FROM notifications WHERE user_id = ? AND read_at IS NULL;

-- name: MarkNotificationsRead :exec
UPDATE notifications SET read_at = ? WHERE user_id = ? AND read_at IS NULL;

-- name: PruneNotifications :exec
DELETE FROM notifications WHERE created_at < ?;

-- ---- evaluator inputs ----

-- Recent large money-out transactions nobody entered by hand: new since created_since, dated
-- on/after from_date, not hidden, not a linked provisional row, and not the posted twin of a
-- pending/email row (that one already alerted or was entered by the user).
-- name: ListLargeTransactions :many
SELECT t.id, t.date, t.amount_cents, t.source, a.name AS account_name,
    CAST(COALESCE(m.name, NULLIF(t.payee, ''), t.description) AS TEXT) AS merchant
FROM transactions t
JOIN accounts a ON a.id = t.account_id
LEFT JOIN merchants m ON m.id = t.merchant_id
WHERE t.household_id = sqlc.arg(household_id) AND t.created_at >= sqlc.arg(created_since)
  AND t.date >= sqlc.arg(from_date) AND t.amount_cents <= sqlc.arg(max_amount)
  AND t.source NOT IN ('manual', 'import') AND t.hidden = 0 AND t.linked_txn_id IS NULL AND a.status != 'ignored'
  AND NOT EXISTS (SELECT 1 FROM transactions p WHERE p.linked_txn_id = t.id)
ORDER BY t.id;

-- Accounts that stopped syncing: dropped from the connection, or their institution needs a
-- new login. since = when it happened (latest matching sync event).
-- name: ListBrokenAccounts :many
SELECT a.id, a.name, a.status, CAST(COALESCE(i.status, '') AS TEXT) AS institution_status,
    CAST(COALESCE((SELECT MAX(e.at) FROM sync_events e
        WHERE e.connection_id = a.connection_id AND e.kind IN ('account_disconnected', 'reauth')), 0) AS INTEGER) AS since
FROM accounts a LEFT JOIN institutions i ON i.id = a.institution_id
WHERE a.household_id = ? AND a.hidden = 0
  AND (a.status = 'disconnected' OR (a.status = 'active' AND i.status = 'reauth'));
