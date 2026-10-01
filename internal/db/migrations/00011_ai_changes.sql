-- +goose Up
-- Every change the email-reading AI made to a transaction, with the value before, so it can
-- be shown and undone.
CREATE TABLE ai_changes (
    id               INTEGER PRIMARY KEY,
    household_id     INTEGER NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    transaction_id   INTEGER NOT NULL REFERENCES transactions(id) ON DELETE CASCADE,
    email_message_id INTEGER REFERENCES email_messages(id) ON DELETE SET NULL,
    field            TEXT    NOT NULL, -- category | notes | tags | needs_review
    old_value        TEXT    NOT NULL DEFAULT '',
    new_value        TEXT    NOT NULL DEFAULT '',
    created_at       INTEGER NOT NULL,
    undone_at        INTEGER
);
CREATE INDEX ai_changes_txn ON ai_changes(transaction_id);

-- +goose Down
DROP TABLE ai_changes;
