-- Every statement here runs inside InTenantTx, so row-level security already
-- restricts it to one organization. The org_id in each statement is for the plan,
-- and for the unique key that decides which row a runner's name means.

-- Announcing and beating are the same write. The key is the name inside an
-- organization: a runner that restarts under its name takes its row back, and one
-- whose row was removed gets it again at the next beat.
-- name: ReportRunner :exec
INSERT INTO runner (org_id, name, version, contract, state, started_at, heartbeat_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (org_id, name) DO UPDATE
SET version      = EXCLUDED.version,
    contract     = EXCLUDED.contract,
    state        = EXCLUDED.state,
    started_at   = EXCLUDED.started_at,
    heartbeat_at = EXCLUDED.heartbeat_at;

-- name: ListRunners :many
SELECT id, org_id, name, version, contract, state, started_at, heartbeat_at
FROM runner
WHERE org_id = $1
ORDER BY name;

-- The oldest worker this organization still hands a run to. Read by the worker
-- just before it claims one.
-- name: GetMinimumRunnerContract :one
SELECT min_runner_contract FROM organization WHERE id = $1;
