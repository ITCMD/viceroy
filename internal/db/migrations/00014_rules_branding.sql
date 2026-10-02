-- +goose Up
-- Rules made from a transaction edit: more conditions (money in/out, day of the month) and
-- more actions (several tags, a goal). add_tag_id is kept for old rows but copied into
-- rule_tags, which is what the pipeline reads now.
ALTER TABLE rules ADD COLUMN direction TEXT NOT NULL DEFAULT ''; -- '' any | out | in
ALTER TABLE rules ADD COLUMN day_min INTEGER; -- day of the month 1-31; day_min > day_max wraps
ALTER TABLE rules ADD COLUMN day_max INTEGER;
ALTER TABLE rules ADD COLUMN set_goal_id INTEGER REFERENCES goals(id) ON DELETE SET NULL;

CREATE TABLE rule_tags (
    rule_id INTEGER NOT NULL REFERENCES rules(id) ON DELETE CASCADE,
    tag_id  INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (rule_id, tag_id)
);
INSERT INTO rule_tags (rule_id, tag_id) SELECT id, add_tag_id FROM rules WHERE add_tag_id IS NOT NULL;
UPDATE rules SET add_tag_id = NULL;

-- Account look: a color (picked by AI from the bank's brand, or by the user) and an optional
-- uploaded logo.
ALTER TABLE accounts ADD COLUMN color TEXT NOT NULL DEFAULT '';        -- #rrggbb
ALTER TABLE accounts ADD COLUMN color_source TEXT NOT NULL DEFAULT ''; -- '' not picked yet | ai | auto | user

CREATE TABLE account_logos (
    account_id INTEGER PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    mime       TEXT    NOT NULL,
    data       BLOB    NOT NULL,
    updated_at INTEGER NOT NULL
);

-- Uncategorized transactions the AI already looked at (and passed on), so the automatic run
-- doesn't ask about them again.
ALTER TABLE transactions ADD COLUMN ai_cat_tried INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE transactions DROP COLUMN ai_cat_tried;
DROP TABLE account_logos;
ALTER TABLE accounts DROP COLUMN color_source;
ALTER TABLE accounts DROP COLUMN color;
UPDATE rules SET add_tag_id = (SELECT MIN(tag_id) FROM rule_tags WHERE rule_id = rules.id);
DROP TABLE rule_tags;
ALTER TABLE rules DROP COLUMN set_goal_id;
ALTER TABLE rules DROP COLUMN day_max;
ALTER TABLE rules DROP COLUMN day_min;
ALTER TABLE rules DROP COLUMN direction;
