-- +goose Up
-- Recurring series are detected on the fly from transaction history (internal/recurring);
-- only the ones the user dismissed are stored, by series key.
CREATE TABLE recurring_dismissed (
    household_id INTEGER NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    key          TEXT    NOT NULL,
    PRIMARY KEY (household_id, key)
);

-- +goose Down
DROP TABLE recurring_dismissed;
