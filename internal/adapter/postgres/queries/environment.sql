-- Every statement runs inside InTenantTx, so row-level security already restricts
-- it to one organization. org_id is named in each WHERE anyway: it is the first
-- column of every index, and a policy is a safety net rather than a query plan.

-- The verification status is not a parameter. An environment is born pending,
-- and the only statements that may move it are the verification's own.
-- name: InsertEnvironment :one
INSERT INTO environment
    (id, org_id, project_id, name, base_url, verification_token,
     verification_status, max_vus, max_duration_s,
     allowed_window_start_minute, allowed_window_end_minute, allowed_window_tz,
     created_at)
VALUES ($1, $2, $3, $4, $5, $6, 'pending', $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: ListEnvironmentsInProject :many
SELECT * FROM environment
WHERE org_id = $1 AND project_id = $2
ORDER BY name, id;

-- The mutable half of an environment. The target's proof and its approval are
-- untouched here: a change that invalidates them goes through the statement
-- below instead.
-- name: UpdateEnvironment :one
UPDATE environment
SET name = $3, base_url = $4, max_vus = $5, max_duration_s = $6,
    allowed_window_start_minute = $7, allowed_window_end_minute = $8,
    allowed_window_tz = $9
WHERE org_id = $1 AND id = $2
RETURNING *;

-- The same, for an environment pointed at another origin. The proof was served by
-- the old one and says nothing about the new one, so it goes back to pending and
-- loses its approver — and until somebody proves and approves it again, no run
-- may be sent there. The verified_at and verified_by pair is cleared with it,
-- because migration 001 holds the three to each other.
-- name: UpdateEnvironmentAndResetVerification :one
UPDATE environment
SET name = $3, base_url = $4, max_vus = $5, max_duration_s = $6,
    allowed_window_start_minute = $7, allowed_window_end_minute = $8,
    allowed_window_tz = $9,
    verification_status = 'pending', verified_at = NULL, verified_by = NULL
WHERE org_id = $1 AND id = $2
RETURNING *;

-- name: CountEnvironmentRuns :one
SELECT count(*) FROM run WHERE org_id = $1 AND environment_id = $2;

-- name: DeleteEnvironment :execrows
DELETE FROM environment WHERE org_id = $1 AND id = $2;

-- What is set, without the values. This is what the API reads.
-- name: ListSecretKeys :many
SELECT id, org_id, environment_id, key, created_at, updated_at FROM secret
WHERE org_id = $1 AND environment_id = $2
ORDER BY key;

-- The ciphertexts, for injection into the generator. Only the worker runs this.
-- name: ListSecrets :many
SELECT * FROM secret
WHERE org_id = $1 AND environment_id = $2
ORDER BY key;

-- One key per environment, and setting it again replaces the ciphertext rather
-- than leaving two rows that disagree.
-- name: UpsertSecret :one
INSERT INTO secret (id, org_id, environment_id, key, value_encrypted, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $6)
ON CONFLICT (org_id, environment_id, key)
DO UPDATE SET value_encrypted = excluded.value_encrypted, updated_at = excluded.updated_at
RETURNING id, org_id, environment_id, key, created_at, updated_at;

-- name: DeleteSecret :execrows
DELETE FROM secret WHERE org_id = $1 AND environment_id = $2 AND key = $3;

-- The outcome of a verification attempt. Two statements rather than one taking
-- the status as a parameter: neither of them can write 'verified', so the only
-- way into that state is the approval below.
-- name: MarkEnvironmentProven :one
UPDATE environment SET verification_status = 'proof_ok'
WHERE org_id = $1 AND id = $2 AND verification_status <> 'verified'
RETURNING *;

-- name: MarkEnvironmentProofFailed :one
UPDATE environment
SET verification_status = 'verification_failed', verified_at = NULL, verified_by = NULL
WHERE org_id = $1 AND id = $2 AND verification_status <> 'verified'
RETURNING *;

-- The human decision that lets load reach a target.
--
-- The WHERE clause carries the rule rather than trusting the caller to have
-- checked it: a target whose proof is not in cannot be approved, whatever the
-- code above believed when it read the row. It also closes the window between
-- that read and this write.
--
-- The three columns move together because migration 001 holds them to each
-- other: a row that says verified with no approver is refused by the schema.
-- name: ApproveEnvironment :one
UPDATE environment
SET verification_status = 'verified', verified_at = $4, verified_by = $3
WHERE org_id = $1 AND id = $2 AND verification_status = 'proof_ok'
RETURNING *;

-- The hosts a project's generators are allowed to reach. Two environments of one
-- project may well point at the same host, so a second approval is not a
-- conflict.
-- name: AllowHostForProject :exec
INSERT INTO project_allowed_host (org_id, project_id, host)
VALUES ($1, $2, $3)
ON CONFLICT (org_id, project_id, host) DO NOTHING;

-- The hosts a project's generators may reach at all. Written by an approval,
-- read by the worker before a run starts.
-- name: ListProjectAllowedHosts :many
SELECT host FROM project_allowed_host
WHERE org_id = $1 AND project_id = $2
ORDER BY host;
