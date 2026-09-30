-- +goose Up
-- Accounts Viceroy creates itself. 'paper_cash' is the built-in wallet for cash spending;
-- disabling it in settings closes and hides it (history is kept).
ALTER TABLE accounts ADD COLUMN builtin TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX accounts_builtin ON accounts(household_id, builtin) WHERE builtin != '';

-- +goose Down
DROP INDEX accounts_builtin;
ALTER TABLE accounts DROP COLUMN builtin;
