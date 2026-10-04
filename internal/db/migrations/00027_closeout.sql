-- +goose Up
-- A month the household closed out: a snapshot of the review (budget vs actual per
-- category, as JSON) and the AI's look at the next month's risk of overspending.
CREATE TABLE budget_closeouts (
    id             INTEGER PRIMARY KEY,
    household_id   INTEGER NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    month          TEXT    NOT NULL, -- YYYY-MM closed out
    closed_at      INTEGER NOT NULL,
    closed_by      INTEGER REFERENCES users(id) ON DELETE SET NULL,
    budget_cents   INTEGER NOT NULL, -- fixed + flexible budget
    actual_cents   INTEGER NOT NULL, -- fixed + flexible spending
    surplus_cents  INTEGER NOT NULL, -- budget − actual when positive, else 0
    review         TEXT    NOT NULL DEFAULT '',
    analysis       TEXT    NOT NULL DEFAULT '',
    analysis_at    INTEGER,
    UNIQUE (household_id, month)
);

-- Where a close-out put the month's unspent money: a raise to a category's budget for one
-- month (category_id + month), or a goal's balance (goal_id). Whatever isn't allocated stays
-- unassigned (cash on hand).
CREATE TABLE closeout_allocations (
    id           INTEGER PRIMARY KEY,
    closeout_id  INTEGER NOT NULL REFERENCES budget_closeouts(id) ON DELETE CASCADE,
    household_id INTEGER NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    category_id  INTEGER REFERENCES categories(id) ON DELETE CASCADE,
    goal_id      INTEGER REFERENCES goals(id) ON DELETE CASCADE,
    month        TEXT    NOT NULL DEFAULT '', -- the budget month raised (category only)
    amount_cents INTEGER NOT NULL,
    CHECK ((category_id IS NULL) != (goal_id IS NULL))
);
CREATE INDEX closeout_allocations_closeout ON closeout_allocations(closeout_id);
CREATE INDEX closeout_allocations_goal ON closeout_allocations(goal_id) WHERE goal_id IS NOT NULL;

-- +goose Down
DROP TABLE closeout_allocations;
DROP TABLE budget_closeouts;
