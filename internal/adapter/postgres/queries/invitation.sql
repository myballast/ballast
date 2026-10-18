-- Every statement here runs inside InTenantTx, so row-level security already
-- restricts it to one organization. The one that does not is invitation_lookup,
-- which is called on its own and written in the repository.

-- name: InsertInvitation :exec
INSERT INTO invitation (id, org_id, project_id, email, role, token_hash, invited_by, expires_at, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- An invitation that expired unused still counts as pending for the unique
-- index, and would keep its address from being invited again. It is closed the
-- way a person would close it, so that the history says so.
-- name: RevokeExpiredInvitations :exec
UPDATE invitation SET revoked_at = $4
WHERE org_id = $1 AND project_id = $2 AND email = $3
  AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at <= $4;

-- name: ListPendingInvitations :many
SELECT id, org_id, project_id, email, role, invited_by, expires_at, accepted_at, revoked_at, created_at
FROM invitation
WHERE org_id = $1 AND project_id = $2 AND accepted_at IS NULL AND revoked_at IS NULL
ORDER BY created_at DESC, id DESC;

-- name: RevokeInvitation :execrows
UPDATE invitation SET revoked_at = $4
WHERE org_id = $1 AND project_id = $2 AND id = $3
  AND accepted_at IS NULL AND revoked_at IS NULL;

-- The use of a link, once. Whoever loses a race for it matches no row.
-- name: UseInvitation :execrows
UPDATE invitation SET accepted_at = $3
WHERE org_id = $1 AND id = $2
  AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > $3;

-- name: FindUserBySubject :one
SELECT id FROM app_user WHERE org_id = $1 AND subject = $2;

-- A person already in the project keeps the role they have: following an
-- invitation never lowers anybody, and raising is an administrator's decision.
-- name: AddMembershipIfAbsent :exec
INSERT INTO membership (org_id, user_id, project_id, role)
VALUES ($1, $2, $3, $4)
ON CONFLICT (org_id, user_id, project_id) DO NOTHING;
