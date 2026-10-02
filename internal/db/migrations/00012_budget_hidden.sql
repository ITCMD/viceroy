-- +goose Up
-- Categories the household doesn't budget for (e.g. Water when it's included in rent). They
-- stay usable on transactions; the budget hides them unless they have activity.
ALTER TABLE categories ADD COLUMN budget_hidden INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE categories DROP COLUMN budget_hidden;
