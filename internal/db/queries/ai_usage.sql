-- name: InsertAIUsage :exec
INSERT INTO ai_usage (household_id, created_at, feature, ref_id, model, local, prompt_tokens, completion_tokens, cost_micros)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- Usage over [from, to) (unix seconds), per feature.
-- name: AIUsageByFeature :many
SELECT feature,
  CAST(COUNT(*) AS INTEGER) AS requests,
  CAST(COALESCE(SUM(cost_micros), 0) AS INTEGER) AS cost_micros,
  CAST(SUM(CASE WHEN cost_micros IS NULL THEN 1 ELSE 0 END) AS INTEGER) AS unpriced,
  CAST(COALESCE(SUM(prompt_tokens + completion_tokens), 0) AS INTEGER) AS tokens
FROM ai_usage
WHERE household_id = sqlc.arg(household_id) AND created_at >= sqlc.arg(from_ts) AND created_at < sqlc.arg(to_ts)
GROUP BY feature ORDER BY cost_micros DESC, feature;

-- name: AIUsageForRef :one
SELECT CAST(COUNT(*) AS INTEGER) AS requests,
  CAST(COALESCE(SUM(cost_micros), 0) AS INTEGER) AS cost_micros,
  CAST(SUM(CASE WHEN cost_micros IS NULL THEN 1 ELSE 0 END) AS INTEGER) AS unpriced
FROM ai_usage WHERE household_id = ? AND feature = ? AND ref_id = ?;
