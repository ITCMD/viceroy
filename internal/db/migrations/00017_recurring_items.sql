-- +goose Up
-- Recurring transactions the household tracks: confirmed from a detected series, marked from
-- a transaction, or entered by hand. Payments are matched on the fly from transaction history
-- (merchant or match text, sign, account, amount), so nothing is stored per payment.
CREATE TABLE recurring_items (
    id            INTEGER PRIMARY KEY,
    household_id  INTEGER NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    name          TEXT    NOT NULL,
    merchant_id   INTEGER REFERENCES merchants(id) ON DELETE SET NULL,
    match_text    TEXT    NOT NULL DEFAULT '', -- matched against the merchant/description (letters and digits only)
    account_id    INTEGER REFERENCES accounts(id) ON DELETE SET NULL, -- NULL = any account
    category_id   INTEGER REFERENCES categories(id) ON DELETE SET NULL,
    amount_cents  INTEGER NOT NULL, -- signed: negative = money out
    amount_varies INTEGER NOT NULL DEFAULT 0,
    cadence       TEXT    NOT NULL, -- weekly | biweekly | semimonthly | monthly | quarterly | yearly
    anchor_date   TEXT    NOT NULL, -- any one due date; the schedule repeats from it
    day2          INTEGER NOT NULL DEFAULT 0, -- semimonthly: the other day of the month
    series_key    TEXT    NOT NULL DEFAULT '', -- the detected series it was confirmed from
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL
);
CREATE INDEX recurring_items_household ON recurring_items(household_id);

-- Notes pinned to a day on the net worth chart, with an optional emoji and transaction.
CREATE TABLE networth_annotations (
    id             INTEGER PRIMARY KEY,
    household_id   INTEGER NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    date           TEXT    NOT NULL,
    label          TEXT    NOT NULL,
    icon           TEXT    NOT NULL DEFAULT '',
    transaction_id INTEGER REFERENCES transactions(id) ON DELETE SET NULL,
    created_at     INTEGER NOT NULL
);
CREATE INDEX networth_annotations_household ON networth_annotations(household_id, date);

-- +goose Down
DROP TABLE networth_annotations;
DROP TABLE recurring_items;
