-- Every statement here runs inside InTenantTx, so row-level security already
-- restricts it to one organization. org_id is still named in each WHERE: it is
-- the first column of every index, and a policy is a safety net rather than a
-- query plan.

-- name: CreateRun :one
INSERT INTO run (
    org_id, project_id, scenario_id, environment_id,
    scenario_version, profile, profile_params, triggered_by, status
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'queued')
RETURNING *;

-- name: GetRun :one
SELECT * FROM run WHERE org_id = $1 AND id = $2;

-- Claiming a run is deliberately naive: no SKIP LOCKED and no environment lock,
-- so two workers can pick up the same queue. The single statement keeps the
-- window small; closing it is the job of the queue ticket.
-- name: ClaimNextQueuedRun :one
UPDATE run
SET status = 'running', started_at = $2, heartbeat_at = $2
WHERE run.org_id = $1
  AND run.id = (
    SELECT queued.id FROM run AS queued
    WHERE queued.org_id = $1 AND queued.status = 'queued'
    ORDER BY queued.created_at
    LIMIT 1
  )
RETURNING *;

-- The status in the WHERE clause is the guard: a run somebody else already
-- finished is not finished twice.
-- name: FinishRun :one
UPDATE run
SET status = $3, ended_at = $4, error_message = $5
WHERE org_id = $1 AND id = $2 AND status = 'running'
RETURNING *;

-- name: GetScenario :one
SELECT * FROM scenario WHERE org_id = $1 AND id = $2;

-- name: GetEnvironment :one
SELECT * FROM environment WHERE org_id = $1 AND id = $2;

-- name: ListScenarioThresholds :many
SELECT * FROM threshold
WHERE org_id = $1 AND scenario_id = $2
ORDER BY metric, expression;

-- One round trip rather than one per measured number: a single run reports a
-- hundred or so.
-- name: InsertRunMetrics :exec
INSERT INTO run_metric (org_id, run_id, name, tag_name, stat, value)
SELECT $1, $2, unnest($3::text[]), unnest($4::text[]), unnest($5::text[]), unnest($6::float8[]);

-- name: InsertRunThresholds :exec
INSERT INTO run_threshold (org_id, run_id, metric, expression, passed)
SELECT $1, $2, unnest($3::text[]), unnest($4::text[]), unnest($5::bool[]);

-- name: ListRunMetrics :many
SELECT * FROM run_metric
WHERE org_id = $1 AND run_id = $2
ORDER BY name, tag_name, stat;

-- name: ListRunThresholds :many
SELECT * FROM run_threshold
WHERE org_id = $1 AND run_id = $2
ORDER BY metric, expression;
