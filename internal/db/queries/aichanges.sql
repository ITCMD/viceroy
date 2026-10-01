-- name: InsertAIChange :exec
INSERT INTO ai_changes (household_id, transaction_id, email_message_id, field, old_value, new_value, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: ListAIChanges :many
SELECT c.*, CAST(COALESCE(m.subject, '') AS TEXT) AS email_subject FROM ai_changes c
LEFT JOIN email_messages m ON m.id = c.email_message_id
WHERE c.transaction_id = ? AND c.household_id = ? AND c.undone_at IS NULL ORDER BY c.id;

-- name: MarkAIChangesUndone :exec
UPDATE ai_changes SET undone_at = ? WHERE transaction_id = ? AND household_id = ? AND undone_at IS NULL;

-- Narrow writes the email AI may make.
-- name: SetTransactionCategoryAI :exec
UPDATE transactions SET category_id = ?, category_source = ?, updated_at = ? WHERE id = ? AND household_id = ?;

-- name: SetTransactionNotes :exec
UPDATE transactions SET notes = ?, updated_at = ? WHERE id = ? AND household_id = ?;

-- name: SetTransactionNeedsReview :exec
UPDATE transactions SET needs_review = ?, updated_at = ? WHERE id = ? AND household_id = ?;

-- name: RemoveTransactionTag :exec
DELETE FROM transaction_tags WHERE transaction_id = ? AND tag_id = ?;
