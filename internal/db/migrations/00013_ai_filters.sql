-- +goose Up
-- Filters the email-reading AI wrote (source = 'ai'), checked by Viceroy before they're saved.
-- Their transactions are flagged for review; editing one makes it the user's.
ALTER TABLE email_filters ADD COLUMN source TEXT NOT NULL DEFAULT 'user';
-- What the AI read from a transaction email (amount, merchant, account, where each value sits),
-- as JSON, so "Create filter" can start from it even when no filter could be built.
ALTER TABLE email_messages ADD COLUMN ai_recipe TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE email_messages DROP COLUMN ai_recipe;
ALTER TABLE email_filters DROP COLUMN source;
