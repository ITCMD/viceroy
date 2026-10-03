-- +goose Up
-- One row per AI request: what it was for, tokens, and the cost OpenRouter reported in
-- millionths of a dollar (NULL = not reported). ref_id is the chat thread for feature 'chat'.
CREATE TABLE ai_usage (
    id                INTEGER PRIMARY KEY,
    household_id      INTEGER NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    created_at        INTEGER NOT NULL,
    feature           TEXT    NOT NULL,
    ref_id            INTEGER,
    model             TEXT    NOT NULL,
    local             INTEGER NOT NULL DEFAULT 0,
    prompt_tokens     INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    cost_micros       INTEGER
);
CREATE INDEX ai_usage_household ON ai_usage(household_id, created_at);
CREATE INDEX ai_usage_ref ON ai_usage(feature, ref_id);

-- +goose Down
DROP TABLE ai_usage;
