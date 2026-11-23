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

-- The project row is locked before the count, so that two creations at once wait
-- for each other and the second one sees the first. Without it, both would read
-- the same count and both would find room.
-- name: LockProjectForApiTokens :one
SELECT id FROM project WHERE org_id = $1 AND id = $2 FOR UPDATE;

-- What has not been revoked counts against the ceiling, expired or not.
-- name: CountLiveApiTokens :one
SELECT count(*) FROM api_token
WHERE org_id = $1 AND project_id = $2 AND revoked_at IS NULL;

-- name: InsertApiToken :exec
INSERT INTO api_token (id, org_id, project_id, name, prefix, token_hash, created_by, expires_at, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- The name of whoever made it, when they still exist: a token outlives its
-- author, and the join must not drop it.
-- name: ListApiTokens :many
SELECT t.id, t.org_id, t.project_id, t.name, t.prefix, t.created_by, t.expires_at,
       t.last_used_at, t.revoked_at, t.created_at,
       coalesce(u.name, '')::text AS created_by_name
FROM api_token t
LEFT JOIN app_user u ON u.org_id = t.org_id AND u.id = t.created_by
WHERE t.org_id = $1 AND t.project_id = $2
ORDER BY t.created_at DESC, t.id DESC;

-- The date of the revocation, once. Whoever loses a race for it, or asks again,
-- matches no row.
-- name: RevokeApiToken :execrows
UPDATE api_token SET revoked_at = $4
WHERE org_id = $1 AND project_id = $2 AND id = $3 AND revoked_at IS NULL;

-- How a token is called, for the screen that says which pipeline launched a run.
-- name: ApiTokenName :one
SELECT name FROM api_token WHERE org_id = $1 AND id = $2;
