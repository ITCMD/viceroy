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
INSERT INTO household_members (household_id, user_id, role) VALUES (?, ?, ?);

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
