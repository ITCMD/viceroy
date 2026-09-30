-- +goose Up
-- A bank-data connection. For SimpleFIN, one access URL (may span many institutions).
CREATE TABLE connections (
    id             INTEGER PRIMARY KEY,
    household_id   INTEGER NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    provider       TEXT    NOT NULL,              -- simplefin
    name           TEXT    NOT NULL,
    secret_enc     BLOB    NOT NULL,              -- encrypted access URL
    status         TEXT    NOT NULL DEFAULT 'active', -- active | error | revoked
    last_error     TEXT    NOT NULL DEFAULT '',
    last_sync_at   INTEGER,                       -- last successful sync (unix)
    synced_through INTEGER,                       -- transactions fetched up to (unix)
    next_sync_at   INTEGER,
    requests_day   TEXT    NOT NULL DEFAULT '',   -- YYYY-MM-DD the counter applies to
    requests_count INTEGER NOT NULL DEFAULT 0,
    created_at     INTEGER NOT NULL
);

-- An institution login inside a connection (SimpleFIN v2 "connection", conn_id).
CREATE TABLE institutions (
    id            INTEGER PRIMARY KEY,
    connection_id INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
    external_id   TEXT    NOT NULL,
    name          TEXT    NOT NULL,
    url           TEXT    NOT NULL DEFAULT '',
    status        TEXT    NOT NULL DEFAULT 'ok',  -- ok | reauth
    last_error    TEXT    NOT NULL DEFAULT '',
    UNIQUE (connection_id, external_id)
);

CREATE TABLE accounts (
    id                   INTEGER PRIMARY KEY,
    household_id         INTEGER NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    connection_id        INTEGER REFERENCES connections(id) ON DELETE SET NULL,
    institution_id       INTEGER REFERENCES institutions(id) ON DELETE SET NULL,
    external_id          TEXT,
    institution_name     TEXT    NOT NULL DEFAULT '',
    provider_name        TEXT    NOT NULL DEFAULT '', -- name as reported by the provider
    name                 TEXT    NOT NULL,            -- display name (user-editable)
    mask                 TEXT    NOT NULL DEFAULT '', -- last 4 digits when known
    type                 TEXT    NOT NULL,            -- see accounts.Type
    currency             TEXT    NOT NULL DEFAULT 'USD',
    balance_cents        INTEGER NOT NULL DEFAULT 0,
    available_cents      INTEGER,
    balance_at           INTEGER,
    status               TEXT    NOT NULL DEFAULT 'active', -- active | review | disconnected | ignored | closed
    review_candidate_id  INTEGER REFERENCES accounts(id) ON DELETE SET NULL,
    include_in_net_worth INTEGER NOT NULL DEFAULT 1,
    hidden               INTEGER NOT NULL DEFAULT 0,
    owner_user_id        INTEGER REFERENCES users(id) ON DELETE SET NULL,
    is_manual            INTEGER NOT NULL DEFAULT 0,
    created_at           INTEGER NOT NULL,
    updated_at           INTEGER NOT NULL
);
CREATE UNIQUE INDEX accounts_external ON accounts(connection_id, external_id) WHERE external_id IS NOT NULL;
CREATE INDEX accounts_household ON accounts(household_id);

-- One row per account per day; the latest balance seen that day wins.
CREATE TABLE balance_snapshots (
    account_id    INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    date          TEXT    NOT NULL, -- YYYY-MM-DD
    balance_cents INTEGER NOT NULL,
    PRIMARY KEY (account_id, date)
);

CREATE TABLE transactions (
    id            INTEGER PRIMARY KEY,
    household_id  INTEGER NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    account_id    INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    external_id   TEXT,
    source        TEXT    NOT NULL,           -- manual | simplefin | email
    date          TEXT    NOT NULL,           -- YYYY-MM-DD (local date of the transaction)
    amount_cents  INTEGER NOT NULL,           -- negative = money out
    description   TEXT    NOT NULL DEFAULT '', -- original statement text
    payee         TEXT    NOT NULL DEFAULT '',
    memo          TEXT    NOT NULL DEFAULT '',
    pending       INTEGER NOT NULL DEFAULT 0,
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL
);
CREATE UNIQUE INDEX transactions_external ON transactions(account_id, external_id) WHERE external_id IS NOT NULL;
CREATE INDEX transactions_household_date ON transactions(household_id, date);
CREATE INDEX transactions_account_date ON transactions(account_id, date);

-- Audit trail of what sync/reconcile did, shown in the connection's history.
CREATE TABLE sync_events (
    id            INTEGER PRIMARY KEY,
    connection_id INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
    at            INTEGER NOT NULL,
    kind          TEXT    NOT NULL, -- sync_ok | sync_error | account_new | account_relinked | account_review | account_disconnected | reauth
    account_id    INTEGER,
    message       TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX sync_events_connection ON sync_events(connection_id, at);

-- +goose Down
DROP TABLE sync_events;
DROP TABLE transactions;
DROP TABLE balance_snapshots;
DROP TABLE accounts;
DROP TABLE institutions;
DROP TABLE connections;
