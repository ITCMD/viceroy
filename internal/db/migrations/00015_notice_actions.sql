-- +goose Up
-- What a filter does with the emails it catches: book a transaction (the default), set the
-- account's balance from a balance summary, or ignore them.
ALTER TABLE email_filters ADD COLUMN action TEXT NOT NULL DEFAULT 'transaction'; -- transaction | balance | ignore
-- Facts the AI read from a notice (e.g. a balance summary) after Viceroy checked them, as JSON
-- (email.Facts), so the notice can offer one-click actions.
ALTER TABLE email_messages ADD COLUMN ai_facts TEXT NOT NULL DEFAULT '';
-- What Viceroy did with the email besides a transaction, e.g. "Set Quicksilver balance to $14.99".
ALTER TABLE email_messages ADD COLUMN applied TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE email_messages DROP COLUMN applied;
ALTER TABLE email_messages DROP COLUMN ai_facts;
ALTER TABLE email_filters DROP COLUMN action;
