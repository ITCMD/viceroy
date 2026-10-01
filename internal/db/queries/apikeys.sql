-- name: InsertAPIKey :one
INSERT INTO api_keys (household_id, user_id, name, prefix, token_hash, scope, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING *;

-- name: ListAPIKeys :many
SELECT k.*, u.name AS user_name FROM api_keys k JOIN users u ON u.id = k.user_id
WHERE k.household_id = ? AND k.revoked_at IS NULL ORDER BY k.id;

-- name: GetAPIKeyByHash :one
SELECT * FROM api_keys WHERE token_hash = ? AND revoked_at IS NULL;

-- name: RevokeAPIKey :execrows
UPDATE api_keys SET revoked_at = ? WHERE id = ? AND household_id = ? AND revoked_at IS NULL;

-- name: TouchAPIKey :exec
UPDATE api_keys SET last_used_at = ? WHERE id = ?;
