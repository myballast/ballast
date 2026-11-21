-- Every statement here runs inside InTenantTx, so row-level security already
-- restricts it to one organization. The one that does not is api_token_lookup,
-- which is called on its own and written in the repository.

-- name: ApiTokenByID :one
SELECT id, org_id, project_id, name, prefix, created_by, expires_at, last_used_at, revoked_at, created_at
FROM api_token
WHERE org_id = $1 AND id = $2;

-- A use is information for whoever reads the list, and a write on every request
-- would be the heaviest thing a busy pipeline did: at most one a minute.
-- name: TouchApiToken :exec
UPDATE api_token SET last_used_at = $3
WHERE org_id = $1 AND id = $2
  AND (last_used_at IS NULL OR last_used_at + interval '1 minute' <= $3);
