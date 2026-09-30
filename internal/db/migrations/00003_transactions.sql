-- +goose Up
-- Budget sections. kind drives budget/report behavior: income | fixed | flexible | non_monthly | transfer.
CREATE TABLE category_groups (
    id           INTEGER PRIMARY KEY,
    household_id INTEGER NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    name         TEXT    NOT NULL,
    kind         TEXT    NOT NULL,
    sort         INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX category_groups_household ON category_groups(household_id);

CREATE TABLE categories (
    id           INTEGER PRIMARY KEY,
    household_id INTEGER NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    group_id     INTEGER NOT NULL REFERENCES category_groups(id) ON DELETE CASCADE,
    name         TEXT    NOT NULL,
    icon         TEXT    NOT NULL DEFAULT '', -- emoji
    sort         INTEGER NOT NULL DEFAULT 0,
    archived     INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX categories_household ON categories(household_id);

-- A cleaned-up payee name. "normalized" is the lowercase alphanumeric key used for lookups.
CREATE TABLE merchants (
    id           INTEGER PRIMARY KEY,
    household_id INTEGER NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    name         TEXT    NOT NULL,
    normalized   TEXT    NOT NULL,
    UNIQUE (household_id, normalized)
);

CREATE TABLE tags (
    id           INTEGER PRIMARY KEY,
    household_id INTEGER NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    name         TEXT    NOT NULL,
    color        TEXT    NOT NULL DEFAULT '',
    UNIQUE (household_id, name)
);

-- User rules, applied in priority order; the first rule that matches wins.
CREATE TABLE rules (
    id            INTEGER PRIMARY KEY,
    household_id  INTEGER NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    priority      INTEGER NOT NULL DEFAULT 0,
    match_field   TEXT    NOT NULL DEFAULT 'merchant', -- merchant | description
    match_op      TEXT    NOT NULL DEFAULT 'contains', -- contains | equals | starts_with
    match_value   TEXT    NOT NULL,
    account_id    INTEGER REFERENCES accounts(id) ON DELETE CASCADE,
    amount_min    INTEGER,  -- cents, compared against abs(amount)
    amount_max    INTEGER,
    set_category_id INTEGER REFERENCES categories(id) ON DELETE SET NULL,
    set_merchant  TEXT    NOT NULL DEFAULT '',
    add_tag_id    INTEGER REFERENCES tags(id) ON DELETE SET NULL,
    set_hidden    INTEGER NOT NULL DEFAULT 0,
    created_at    INTEGER NOT NULL
);
CREATE INDEX rules_household ON rules(household_id, priority);

ALTER TABLE transactions ADD COLUMN merchant_id INTEGER REFERENCES merchants(id) ON DELETE SET NULL;
ALTER TABLE transactions ADD COLUMN category_id INTEGER REFERENCES categories(id) ON DELETE SET NULL;
ALTER TABLE transactions ADD COLUMN category_source TEXT NOT NULL DEFAULT ''; -- rule | history | ai | user | linked
ALTER TABLE transactions ADD COLUMN notes TEXT NOT NULL DEFAULT '';
ALTER TABLE transactions ADD COLUMN hidden INTEGER NOT NULL DEFAULT 0;
ALTER TABLE transactions ADD COLUMN needs_review INTEGER NOT NULL DEFAULT 0;
-- Manual pending entries and email alerts: stand-ins until the posted transaction arrives.
ALTER TABLE transactions ADD COLUMN provisional INTEGER NOT NULL DEFAULT 0;
-- On a provisional row: the posted transaction it was matched to. Linked provisional rows
-- are left out of lists and totals.
ALTER TABLE transactions ADD COLUMN linked_txn_id INTEGER REFERENCES transactions(id) ON DELETE SET NULL;
ALTER TABLE transactions ADD COLUMN linked_at INTEGER;
ALTER TABLE transactions ADD COLUMN goal_id INTEGER;
ALTER TABLE transactions ADD COLUMN owner_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL;
CREATE INDEX transactions_linked ON transactions(linked_txn_id) WHERE linked_txn_id IS NOT NULL;
CREATE INDEX transactions_merchant ON transactions(merchant_id);

CREATE TABLE transaction_tags (
    transaction_id INTEGER NOT NULL REFERENCES transactions(id) ON DELETE CASCADE,
    tag_id         INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (transaction_id, tag_id)
);

-- Pairs the user unlinked; the linker never pairs them again.
CREATE TABLE link_blacklist (
    provisional_id INTEGER NOT NULL REFERENCES transactions(id) ON DELETE CASCADE,
    posted_id      INTEGER NOT NULL REFERENCES transactions(id) ON DELETE CASCADE,
    PRIMARY KEY (provisional_id, posted_id)
);

-- +goose Down
DROP TABLE link_blacklist;
DROP TABLE transaction_tags;
DROP INDEX transactions_merchant;
DROP INDEX transactions_linked;
ALTER TABLE transactions DROP COLUMN owner_user_id;
ALTER TABLE transactions DROP COLUMN goal_id;
ALTER TABLE transactions DROP COLUMN linked_at;
ALTER TABLE transactions DROP COLUMN linked_txn_id;
ALTER TABLE transactions DROP COLUMN provisional;
ALTER TABLE transactions DROP COLUMN needs_review;
ALTER TABLE transactions DROP COLUMN hidden;
ALTER TABLE transactions DROP COLUMN notes;
ALTER TABLE transactions DROP COLUMN category_source;
ALTER TABLE transactions DROP COLUMN category_id;
ALTER TABLE transactions DROP COLUMN merchant_id;
DROP TABLE rules;
DROP TABLE tags;
DROP TABLE merchants;
DROP TABLE categories;
DROP TABLE category_groups;
