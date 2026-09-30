-- +goose Up
-- Web Push subscriptions, one per browser/device a user enabled notifications on.
CREATE TABLE push_subscriptions (
    id         INTEGER PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    endpoint   TEXT    NOT NULL UNIQUE,
    p256dh     TEXT    NOT NULL,
    auth       TEXT    NOT NULL,
    user_agent TEXT    NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL
);
CREATE INDEX push_subscriptions_user ON push_subscriptions(user_id);

-- Per-user notification settings as notify.Prefs JSON. No row = defaults.
CREATE TABLE notification_prefs (
    user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    prefs   TEXT    NOT NULL
);

-- Every alert raised for a user (the in-app list). dedupe_key makes each alert fire once,
-- e.g. over:<category>:<YYYY-MM>.
CREATE TABLE notifications (
    id         INTEGER PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind       TEXT    NOT NULL, -- over_budget | pacing | large_txn | disconnected | test
    dedupe_key TEXT    NOT NULL,
    title      TEXT    NOT NULL,
    body       TEXT    NOT NULL DEFAULT '',
    url        TEXT    NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    read_at    INTEGER,
    UNIQUE (user_id, dedupe_key)
);
CREATE INDEX notifications_user ON notifications(user_id, created_at);

CREATE TABLE chat_threads (
    id         INTEGER PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title      TEXT    NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX chat_threads_user ON chat_threads(user_id, updated_at);

-- Messages in OpenRouter (OpenAI) chat format. tool_calls is the assistant's JSON array;
-- tool rows carry tool_call_id and the tool's JSON result in content.
CREATE TABLE chat_messages (
    id           INTEGER PRIMARY KEY,
    thread_id    INTEGER NOT NULL REFERENCES chat_threads(id) ON DELETE CASCADE,
    role         TEXT    NOT NULL, -- user | assistant | tool
    content      TEXT    NOT NULL DEFAULT '',
    tool_calls   TEXT    NOT NULL DEFAULT '',
    tool_call_id TEXT    NOT NULL DEFAULT '',
    created_at   INTEGER NOT NULL
);
CREATE INDEX chat_messages_thread ON chat_messages(thread_id, id);

-- +goose Down
DROP TABLE chat_messages;
DROP TABLE chat_threads;
DROP TABLE notifications;
DROP TABLE notification_prefs;
DROP TABLE push_subscriptions;
