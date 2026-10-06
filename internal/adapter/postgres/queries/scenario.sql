-- Every statement runs inside InTenantTx, so row-level security already restricts
-- it to one organization. org_id is named in each WHERE anyway: it is the first
-- column of every index, and a policy is a safety net rather than a query plan.

-- name: InsertScenario :one
INSERT INTO scenario
    (id, org_id, project_id, name, script, version,
     default_profile, default_profile_params, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9)
RETURNING *;

-- Nothing ever updates a row of this table. A run names the version it executed,
-- and that has to keep meaning what it meant.
-- name: InsertScenarioVersion :one
INSERT INTO scenario_version
    (org_id, scenario_id, version, script, default_profile,
     default_profile_params, created_by, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: ListScenariosInProject :many
SELECT * FROM scenario
WHERE org_id = $1 AND project_id = $2
ORDER BY name, id;

-- A name is not something a run executed, so renaming writes no version.
-- name: RenameScenario :one
UPDATE scenario SET name = $3, updated_at = $4
WHERE org_id = $1 AND id = $2
RETURNING *;

-- The head of the scenario follows the version just written. The two are
-- deliberately duplicated — the worker reads the head — and the repository writes
-- them in one transaction.
-- name: PointScenarioAtVersion :one
UPDATE scenario
SET script = $3, version = $4, default_profile = $5,
    default_profile_params = $6, updated_at = $7
WHERE org_id = $1 AND id = $2
RETURNING *;

-- name: ListScenarioVersions :many
SELECT * FROM scenario_version
WHERE org_id = $1 AND scenario_id = $2
ORDER BY version DESC;

-- name: CountScenarioRuns :one
SELECT count(*) FROM run WHERE org_id = $1 AND scenario_id = $2;

-- name: DeleteScenario :execrows
DELETE FROM scenario WHERE org_id = $1 AND id = $2;

-- name: InsertThreshold :one
INSERT INTO threshold (id, org_id, scenario_id, metric, expression)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: DeleteThreshold :execrows
DELETE FROM threshold WHERE org_id = $1 AND scenario_id = $2 AND id = $3;
