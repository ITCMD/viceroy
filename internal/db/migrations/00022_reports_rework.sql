-- +goose Up
-- Debt Free Future: what a debt costs. apr_bps is the yearly rate in basis points
-- (2499 = 24.99%); min_payment_cents the monthly minimum. NULL = not entered.
ALTER TABLE accounts ADD COLUMN apr_bps INTEGER;
ALTER TABLE accounts ADD COLUMN min_payment_cents INTEGER;
-- "Discuss" chats started from a report keep what was on screen (JSON) for the model.
ALTER TABLE chat_threads ADD COLUMN context TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE chat_threads DROP COLUMN context;
ALTER TABLE accounts DROP COLUMN min_payment_cents;
ALTER TABLE accounts DROP COLUMN apr_bps;
