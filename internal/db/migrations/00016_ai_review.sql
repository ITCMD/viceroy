-- +goose Up
-- AI category picks no longer need review unless the household turns that on
-- (ai.categorize_review). Clear the flag the categorizer left on its earlier picks; picks the
-- email AI made (logged in ai_changes) keep theirs.
UPDATE transactions SET needs_review = 0
WHERE category_source = 'ai' AND ai_cat_tried = 1 AND needs_review = 1
  AND id NOT IN (SELECT transaction_id FROM ai_changes);

-- +goose Down
SELECT 1;
