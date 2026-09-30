-- +goose Up
-- IMAP mailboxes watched for bank transaction alerts. Viceroy only reads (EXAMINE + BODY.PEEK).
CREATE TABLE email_mailboxes (
    id             INTEGER PRIMARY KEY,
    household_id   INTEGER NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    name           TEXT    NOT NULL DEFAULT '',
    host           TEXT    NOT NULL,
    port           INTEGER NOT NULL DEFAULT 993,
    security       TEXT    NOT NULL DEFAULT 'tls', -- tls | starttls | none
    username       TEXT    NOT NULL,
    password_enc   BLOB    NOT NULL,
    folder         TEXT    NOT NULL DEFAULT 'INBOX',
    enabled        INTEGER NOT NULL DEFAULT 1,
    uid_validity   INTEGER NOT NULL DEFAULT 0,
    last_uid       INTEGER NOT NULL DEFAULT 0,
    status         TEXT    NOT NULL DEFAULT 'new', -- new | ok | error
    last_error     TEXT    NOT NULL DEFAULT '',
    last_checked_at INTEGER,
    created_at     INTEGER NOT NULL
);
CREATE INDEX email_mailboxes_household ON email_mailboxes(household_id);

-- Filters route an alert email to one account; the first enabled match by priority wins.
CREATE TABLE email_filters (
    id            INTEGER PRIMARY KEY,
    household_id  INTEGER NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    name          TEXT    NOT NULL DEFAULT '',
    priority      INTEGER NOT NULL DEFAULT 0,
    enabled       INTEGER NOT NULL DEFAULT 1,
    sender        TEXT    NOT NULL DEFAULT '', -- exact address, or a domain (matches any address at it)
    subject_match TEXT    NOT NULL DEFAULT '',
    body_match    TEXT    NOT NULL DEFAULT '',
    use_regex     INTEGER NOT NULL DEFAULT 0,  -- subject/body matches are regexes instead of "contains"
    account_id    INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    parser        TEXT    NOT NULL DEFAULT 'generic', -- built-in template name, or 'custom'
    custom_parser TEXT    NOT NULL DEFAULT '',        -- JSON field specs when parser = 'custom'
    sign          TEXT    NOT NULL DEFAULT 'debit',   -- debit (money out) | credit
    created_at    INTEGER NOT NULL
);
CREATE INDEX email_filters_household ON email_filters(household_id, priority);

CREATE TABLE email_messages (
    id             INTEGER PRIMARY KEY,
    household_id   INTEGER NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    mailbox_id     INTEGER REFERENCES email_mailboxes(id) ON DELETE SET NULL,
    message_id     TEXT    NOT NULL,
    uid            INTEGER NOT NULL DEFAULT 0,
    from_addr      TEXT    NOT NULL DEFAULT '',
    from_name      TEXT    NOT NULL DEFAULT '',
    subject        TEXT    NOT NULL DEFAULT '',
    received_at    INTEGER NOT NULL,
    body_text      TEXT    NOT NULL DEFAULT '', -- plain text (HTML converted); pruned after 90 days
    status         TEXT    NOT NULL,            -- unrouted | parsed | parse_failed | ignored
    filter_id      INTEGER REFERENCES email_filters(id) ON DELETE SET NULL,
    transaction_id INTEGER REFERENCES transactions(id) ON DELETE SET NULL,
    error          TEXT    NOT NULL DEFAULT '',
    created_at     INTEGER NOT NULL,
    UNIQUE (household_id, message_id)
);
CREATE INDEX email_messages_status ON email_messages(household_id, status, received_at);
CREATE INDEX email_messages_txn ON email_messages(transaction_id) WHERE transaction_id IS NOT NULL;

-- +goose Down
DROP TABLE email_messages;
DROP TABLE email_filters;
DROP TABLE email_mailboxes;
