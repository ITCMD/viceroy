-- name: CreateChatThread :one
INSERT INTO chat_threads (user_id, title, created_at, updated_at) VALUES (?, ?, ?, ?) RETURNING *;

-- name: GetChatThread :one
SELECT * FROM chat_threads WHERE id = ? AND user_id = ?;

-- name: ListChatThreads :many
SELECT * FROM chat_threads WHERE user_id = ? ORDER BY updated_at DESC, id DESC LIMIT 50;

-- name: TouchChatThread :exec
UPDATE chat_threads SET updated_at = ? WHERE id = ?;

-- name: RenameChatThread :exec
UPDATE chat_threads SET title = ? WHERE id = ? AND user_id = ?;

-- name: DeleteChatThread :exec
DELETE FROM chat_threads WHERE id = ? AND user_id = ?;

-- name: InsertChatMessage :one
INSERT INTO chat_messages (thread_id, role, content, tool_calls, tool_call_id, created_at)
VALUES (?, ?, ?, ?, ?, ?) RETURNING *;

-- name: ListChatMessages :many
SELECT * FROM chat_messages WHERE thread_id = ? ORDER BY id;
