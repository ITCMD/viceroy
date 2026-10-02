-- name: CountUsers :one
SELECT COUNT(*) FROM users;

-- name: CreateUser :one
INSERT INTO users (email, name, password_hash, is_admin, created_at)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = ?;

-- name: GetUser :one
SELECT * FROM users WHERE id = ?;

-- name: CreateHousehold :one
INSERT INTO households (name, created_at) VALUES (?, ?) RETURNING *;

-- name: AddHouseholdMember :exec
INSERT INTO household_members (household_id, user_id, role, joined_at) VALUES (?, ?, ?, ?);

-- name: GetUserHousehold :one
SELECT h.* FROM households h
JOIN household_members m ON m.household_id = h.id
WHERE m.user_id = ?
ORDER BY h.id
LIMIT 1;

-- name: CreateSession :exec
INSERT INTO sessions (token_hash, user_id, created_at, expires_at, last_seen_at, user_agent, ip)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: GetSession :one
SELECT * FROM sessions WHERE token_hash = ? AND expires_at > ?;

-- name: TouchSession :exec
UPDATE sessions SET last_seen_at = ?, expires_at = ? WHERE token_hash = ?;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE token_hash = ?;

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions WHERE expires_at <= ?;

-- ---- household management ----

-- name: RenameHousehold :exec
UPDATE households SET name = ? WHERE id = ?;

-- name: ListHouseholdMembers :many
SELECT u.id, u.name, u.email, u.is_admin, m.role, m.joined_at
FROM users u JOIN household_members m ON m.user_id = u.id
WHERE m.household_id = ? ORDER BY m.joined_at, u.id;

-- name: CountHouseholdAdmins :one
SELECT COUNT(*) FROM users u JOIN household_members m ON m.user_id = u.id
WHERE m.household_id = ? AND u.is_admin = 1;

-- name: SetUserAdmin :exec
UPDATE users SET is_admin = ? WHERE id = ?;

-- name: SetMemberRole :exec
UPDATE household_members SET role = ? WHERE household_id = ? AND user_id = ?;

-- name: SetUserPassword :exec
UPDATE users SET password_hash = ? WHERE id = ?;

-- name: DeleteUser :exec
DELETE FROM users WHERE id = ?;

-- name: DeleteUserSessions :exec
DELETE FROM sessions WHERE user_id = ?;

-- name: InsertInvite :one
INSERT INTO household_invites (household_id, user_id, label, token_hash, created_by, created_at, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING *;

-- name: ListOpenInvites :many
SELECT i.*, COALESCE(c.name, '') AS created_by_name, COALESCE(t.name, '') AS user_name
FROM household_invites i
LEFT JOIN users c ON c.id = i.created_by
LEFT JOIN users t ON t.id = i.user_id
WHERE i.household_id = ? AND i.used_at IS NULL AND i.revoked_at IS NULL AND i.expires_at > ?
ORDER BY i.id;

-- name: GetOpenInviteByHash :one
SELECT i.*, h.name AS household_name, COALESCE(c.name, '') AS created_by_name, COALESCE(t.email, '') AS user_email
FROM household_invites i
JOIN households h ON h.id = i.household_id
LEFT JOIN users c ON c.id = i.created_by
LEFT JOIN users t ON t.id = i.user_id
WHERE i.token_hash = ? AND i.used_at IS NULL AND i.revoked_at IS NULL AND i.expires_at > ?;

-- name: UseInvite :execrows
UPDATE household_invites SET used_at = ? WHERE id = ? AND used_at IS NULL AND revoked_at IS NULL;

-- name: RevokeInvite :execrows
UPDATE household_invites SET revoked_at = ? WHERE id = ? AND household_id = ? AND used_at IS NULL AND revoked_at IS NULL;

-- name: RevokeUserResets :exec
UPDATE household_invites SET revoked_at = ? WHERE user_id = ? AND used_at IS NULL AND revoked_at IS NULL;

-- name: SetUserName :exec
UPDATE users SET name = ? WHERE id = ?;

-- name: DeleteOtherSessions :exec
DELETE FROM sessions WHERE user_id = ? AND token_hash != ?;
