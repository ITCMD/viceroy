-- +goose Up
-- Some banks report a balance with the wrong sign through SimpleFIN (e.g. an overdrawn
-- checking account as positive); the user can flip it per account.
ALTER TABLE accounts ADD COLUMN invert_balance INTEGER NOT NULL DEFAULT 0;
-- When set, SimpleFIN offered this account after the connection was set up and it waits for
-- the user to add it (status 'ignored' until then).
ALTER TABLE accounts ADD COLUMN offered_at INTEGER;
-- 1 = accounts that appear on the Bridge later are added without asking.
ALTER TABLE connections ADD COLUMN auto_add_new INTEGER NOT NULL DEFAULT 0;
-- The Wishlist goal's icon was the same gift as the Gifts category.
UPDATE goals SET icon = '🌠' WHERE builtin = 'wishlist' AND icon = '🎁';

-- +goose Down
UPDATE goals SET icon = '🎁' WHERE builtin = 'wishlist' AND icon = '🌠';
ALTER TABLE connections DROP COLUMN auto_add_new;
ALTER TABLE accounts DROP COLUMN offered_at;
ALTER TABLE accounts DROP COLUMN invert_balance;
