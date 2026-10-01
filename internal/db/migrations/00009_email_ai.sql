-- +goose Up
-- Optional AI reading of emails no filter caught (per mailbox, limited to listed senders).
ALTER TABLE email_mailboxes ADD COLUMN ai_read INTEGER NOT NULL DEFAULT 0;
ALTER TABLE email_mailboxes ADD COLUMN ai_senders TEXT NOT NULL DEFAULT ''; -- one address or domain per line; '' = any sender

ALTER TABLE email_messages ADD COLUMN ai_status TEXT NOT NULL DEFAULT ''; -- '' (not for AI) | pending | done | failed
ALTER TABLE email_messages ADD COLUMN ai_kind TEXT NOT NULL DEFAULT '';   -- see email.AIKinds
ALTER TABLE email_messages ADD COLUMN ai_summary TEXT NOT NULL DEFAULT '';
CREATE INDEX email_messages_ai ON email_messages(ai_status) WHERE ai_status = 'pending';

-- Bill events read from emails: a payment due, a payment scheduled, a payment received.
CREATE TABLE account_bills (
    id               INTEGER PRIMARY KEY,
    household_id     INTEGER NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    account_id       INTEGER REFERENCES accounts(id) ON DELETE SET NULL, -- NULL when the email named no known account
    kind             TEXT    NOT NULL, -- due | scheduled | paid
    amount_cents     INTEGER,          -- statement balance, or the payment amount
    minimum_cents    INTEGER,          -- minimum payment (due only)
    date             TEXT,             -- due date, scheduled date or payment date (YYYY-MM-DD)
    summary          TEXT    NOT NULL DEFAULT '',
    email_message_id INTEGER REFERENCES email_messages(id) ON DELETE SET NULL,
    created_at       INTEGER NOT NULL
);
CREATE INDEX account_bills_account ON account_bills(account_id, created_at);
CREATE INDEX account_bills_household ON account_bills(household_id, created_at);

-- +goose Down
DROP TABLE account_bills;
DROP INDEX email_messages_ai;
ALTER TABLE email_messages DROP COLUMN ai_summary;
ALTER TABLE email_messages DROP COLUMN ai_kind;
ALTER TABLE email_messages DROP COLUMN ai_status;
ALTER TABLE email_mailboxes DROP COLUMN ai_senders;
ALTER TABLE email_mailboxes DROP COLUMN ai_read;
