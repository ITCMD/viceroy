-- +goose Up
-- Per-household key/value settings (budget defaults, week start, pay schedule...).
CREATE TABLE household_settings (
    household_id INTEGER NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    key          TEXT    NOT NULL,
    value        TEXT    NOT NULL,
    PRIMARY KEY (household_id, key)
);

-- When in the month a category's money is usually spent, as budget.Chunk JSON. '' = even.
ALTER TABLE categories ADD COLUMN chunk TEXT NOT NULL DEFAULT '';

CREATE TABLE goals (
    id             INTEGER PRIMARY KEY,
    household_id   INTEGER NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    name           TEXT    NOT NULL,
    icon           TEXT    NOT NULL DEFAULT '',
    target_cents   INTEGER NOT NULL DEFAULT 0,
    target_date    TEXT,              -- YYYY-MM-DD, optional
    starting_cents INTEGER NOT NULL DEFAULT 0,
    archived       INTEGER NOT NULL DEFAULT 0,
    sort           INTEGER NOT NULL DEFAULT 0,
    created_at     INTEGER NOT NULL
);
CREATE INDEX goals_household ON goals(household_id);
CREATE INDEX transactions_goal ON transactions(goal_id) WHERE goal_id IS NOT NULL;

-- A month's budget for one category or one goal (exactly one is set). forward = also applies
-- to later months that have no row of their own.
CREATE TABLE budget_amounts (
    id           INTEGER PRIMARY KEY,
    household_id INTEGER NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    category_id  INTEGER REFERENCES categories(id) ON DELETE CASCADE,
    goal_id      INTEGER REFERENCES goals(id) ON DELETE CASCADE,
    month        TEXT    NOT NULL, -- YYYY-MM
    amount_cents INTEGER NOT NULL,
    forward      INTEGER NOT NULL DEFAULT 0,
    CHECK ((category_id IS NULL) != (goal_id IS NULL))
);
CREATE INDEX budget_amounts_household ON budget_amounts(household_id, month);

-- +goose Down
DROP TABLE budget_amounts;
DROP INDEX transactions_goal;
DROP TABLE goals;
ALTER TABLE categories DROP COLUMN chunk;
DROP TABLE household_settings;
