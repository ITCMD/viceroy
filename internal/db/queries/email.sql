-- name: ListMailboxes :many
SELECT * FROM email_mailboxes WHERE household_id = ? ORDER BY id;

-- name: ListEnabledMailboxes :many
SELECT * FROM email_mailboxes WHERE enabled = 1 ORDER BY id;

-- name: GetMailbox :one
SELECT * FROM email_mailboxes WHERE id = ? AND household_id = ?;

-- name: GetMailboxByID :one
SELECT * FROM email_mailboxes WHERE id = ?;

-- name: InsertMailbox :one
INSERT INTO email_mailboxes (household_id, name, host, port, security, username, password_enc, folder, enabled, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING *;

-- name: UpdateMailbox :exec
UPDATE email_mailboxes SET name = ?, host = ?, port = ?, security = ?, username = ?, password_enc = ?, folder = ?, enabled = ?
WHERE id = ? AND household_id = ?;

-- Changing the server or folder starts over from a fresh UID window.
-- name: ResetMailboxCursor :exec
UPDATE email_mailboxes SET uid_validity = 0, last_uid = 0, status = 'new', last_error = '' WHERE id = ?;

-- name: SetMailboxCursor :exec
UPDATE email_mailboxes SET uid_validity = ?, last_uid = ? WHERE id = ?;

-- name: SetMailboxStatus :exec
UPDATE email_mailboxes SET status = ?, last_error = ?, last_checked_at = ? WHERE id = ?;

-- name: DeleteMailbox :exec
DELETE FROM email_mailboxes WHERE id = ? AND household_id = ?;

-- name: ListEmailFilters :many
SELECT * FROM email_filters WHERE household_id = ? ORDER BY priority, id;

-- name: GetEmailFilter :one
SELECT * FROM email_filters WHERE id = ? AND household_id = ?;

-- name: InsertEmailFilter :one
INSERT INTO email_filters (household_id, name, priority, enabled, sender, subject_match, body_match, use_regex,
    account_id, parser, custom_parser, sign, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING *;

-- name: UpdateEmailFilter :exec
UPDATE email_filters SET name = ?, priority = ?, enabled = ?, sender = ?, subject_match = ?, body_match = ?,
    use_regex = ?, account_id = ?, parser = ?, custom_parser = ?, sign = ?
WHERE id = ? AND household_id = ?;

-- name: DeleteEmailFilter :exec
DELETE FROM email_filters WHERE id = ? AND household_id = ?;

-- name: NextEmailFilterPriority :one
SELECT CAST(COALESCE(MAX(priority), 0) + 1 AS INTEGER) FROM email_filters WHERE household_id = ?;

-- name: InsertEmailMessage :one
INSERT INTO email_messages (household_id, mailbox_id, message_id, uid, from_addr, from_name, subject,
    received_at, body_text, status, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'unrouted', ?)
ON CONFLICT (household_id, message_id) DO NOTHING
RETURNING id;

-- name: GetEmailMessage :one
SELECT * FROM email_messages WHERE id = ? AND household_id = ?;

-- name: SetEmailMessageResult :exec
UPDATE email_messages SET status = ?, filter_id = ?, transaction_id = ?, error = ? WHERE id = ?;

-- name: ListEmailMessages :many
SELECT m.id, m.mailbox_id, m.from_addr, m.from_name, m.subject, m.received_at, m.status, m.filter_id,
    m.transaction_id, m.error, m.ai_status, m.ai_kind, m.ai_summary, COALESCE(f.name, '') AS filter_name
FROM email_messages m LEFT JOIN email_filters f ON f.id = m.filter_id
WHERE m.household_id = sqlc.arg(household_id) AND (sqlc.arg(status) = '' OR m.status = sqlc.arg(status))
ORDER BY m.received_at DESC, m.id DESC
LIMIT sqlc.arg(lim);

-- Recent messages with bodies, for re-routing and filter previews.
-- name: ListRecentEmailBodies :many
SELECT * FROM email_messages
WHERE household_id = sqlc.arg(household_id) AND body_text != ''
  AND (sqlc.arg(only_open) = 0 OR status IN ('unrouted', 'parse_failed'))
ORDER BY received_at DESC, id DESC
LIMIT sqlc.arg(lim);

-- name: CountOpenEmailMessages :one
SELECT
    CAST(COALESCE(SUM(status = 'unrouted'), 0) AS INTEGER) AS unrouted,
    CAST(COALESCE(SUM(status = 'parse_failed'), 0) AS INTEGER) AS parse_failed
FROM email_messages WHERE household_id = ?;

-- name: GetEmailMessageForTransaction :one
SELECT id, from_addr, from_name, subject, received_at FROM email_messages WHERE transaction_id = ? LIMIT 1;

-- name: PruneEmailBodies :exec
UPDATE email_messages SET body_text = '' WHERE received_at < ? AND body_text != '';

-- name: InsertEmailTransaction :one
INSERT INTO transactions (
    household_id, account_id, source, date, amount_cents, description, payee, pending, provisional,
    created_at, updated_at
) VALUES (?, ?, 'email', ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id;

-- Posted rows a new provisional entry could be linked to (the reverse of ListLinkCandidates,
-- for an email alert that arrives after the bank already posted the purchase).
-- name: ListLinkTargets :many
SELECT t.id, t.date, t.amount_cents, t.description, t.payee, COALESCE(m.name, '') AS merchant_name
FROM transactions t LEFT JOIN merchants m ON m.id = t.merchant_id
WHERE t.account_id = sqlc.arg(account_id) AND t.provisional = 0
  AND t.amount_cents >= sqlc.arg(amount_lo) AND t.amount_cents <= sqlc.arg(amount_hi)
  AND t.date >= sqlc.arg(date_lo) AND t.date <= sqlc.arg(date_hi)
  AND NOT EXISTS (SELECT 1 FROM transactions p WHERE p.linked_txn_id = t.id)
  AND NOT EXISTS (SELECT 1 FROM link_blacklist b WHERE b.posted_id = t.id AND b.provisional_id = sqlc.arg(provisional_id));

-- ---- AI reading ----

-- name: SetMailboxAI :exec
UPDATE email_mailboxes SET ai_read = ?, ai_senders = ? WHERE id = ? AND household_id = ?;

-- name: SetEmailAIStatus :exec
UPDATE email_messages SET ai_status = ? WHERE id = ?;

-- name: SetEmailAIResult :exec
UPDATE email_messages SET ai_status = ?, ai_kind = ?, ai_summary = ?, status = ? WHERE id = ?;

-- Unrouted messages waiting for the AI, oldest first.
-- name: ListPendingAIEmails :many
SELECT * FROM email_messages WHERE ai_status = 'pending' AND status = 'unrouted'
ORDER BY received_at, id LIMIT ?;

-- name: InsertAccountBill :one
INSERT INTO account_bills (household_id, account_id, kind, amount_cents, minimum_cents, date, summary, email_message_id, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING *;

-- Bill events from the last 75 days, newest first.
-- name: ListRecentBills :many
SELECT * FROM account_bills WHERE household_id = ? AND created_at >= ? ORDER BY created_at DESC, id DESC;
