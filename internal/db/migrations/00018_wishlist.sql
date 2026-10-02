-- +goose Up
-- Things the household wants to buy, budgeted through the built-in Wishlist goal.
CREATE TABLE wishlist_items (
    id               INTEGER PRIMARY KEY,
    household_id     INTEGER NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    title            TEXT    NOT NULL,
    url              TEXT    NOT NULL DEFAULT '', -- '' = no link
    store            TEXT    NOT NULL DEFAULT '', -- domain such as amazon.com
    price_cents      INTEGER,                     -- NULL = unknown
    price_source     TEXT    NOT NULL DEFAULT '', -- '' | fetched | user
    price_checked_at INTEGER,
    image_source_url TEXT    NOT NULL DEFAULT '',
    stars            INTEGER NOT NULL DEFAULT 3,  -- 1-5
    saves_money      INTEGER NOT NULL DEFAULT 0,
    notes            TEXT    NOT NULL DEFAULT '',
    added_by         INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at       INTEGER NOT NULL,
    bought_at        TEXT,                        -- YYYY-MM-DD; NULL = still wanted
    bought_txn_id    INTEGER REFERENCES transactions(id) ON DELETE SET NULL
);
CREATE INDEX wishlist_items_household ON wishlist_items(household_id);

CREATE TABLE wishlist_images (
    item_id    INTEGER PRIMARY KEY REFERENCES wishlist_items(id) ON DELETE CASCADE,
    mime       TEXT    NOT NULL,
    data       BLOB    NOT NULL,
    updated_at INTEGER NOT NULL
);

-- Who wants each item.
CREATE TABLE wishlist_wanters (
    item_id INTEGER NOT NULL REFERENCES wishlist_items(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (item_id, user_id)
);

-- 'wishlist' marks the household's Wishlist goal.
ALTER TABLE goals ADD COLUMN builtin TEXT NOT NULL DEFAULT '';

-- A transaction assigned to a goal is a contribution, or with this set, money spent from the
-- goal (it lowers the balance and isn't counted as a contribution).
ALTER TABLE transactions ADD COLUMN goal_withdrawal INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE transactions DROP COLUMN goal_withdrawal;
ALTER TABLE goals DROP COLUMN builtin;
DROP TABLE wishlist_wanters;
DROP TABLE wishlist_images;
DROP TABLE wishlist_items;
