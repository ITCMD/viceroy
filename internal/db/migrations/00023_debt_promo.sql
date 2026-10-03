-- +goose Up
-- Debt Free Future: an intro rate. The debt accrues no interest through promo_until
-- (YYYY-MM-DD), then apr_bps applies. NULL = no promo.
ALTER TABLE accounts ADD COLUMN promo_until TEXT;

-- +goose Down
ALTER TABLE accounts DROP COLUMN promo_until;
