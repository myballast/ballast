-- Every statement runs inside InTenantTx, so row-level security already
-- restricts it to one organization. org_id is named in each WHERE anyway: it is
-- the first column of every index, and a policy is a safety net rather than a
-- query plan.

-- created_at comes from the injected clock rather than now(), so that what a
-- project records about when it was created can be controlled in a test.
-- name: InsertProject :one
INSERT INTO project (id, org_id, name, slug, created_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetProject :one
SELECT * FROM project WHERE org_id = $1 AND id = $2;

-- A person sees the projects they belong to, and no others. This is a filter
-- rather than a permission check: there is nothing to refuse, the rows simply
-- are not theirs.
-- name: ListProjectsOfMember :many
SELECT p.*
FROM project p
JOIN membership m ON m.org_id = p.org_id AND m.project_id = p.id
WHERE m.org_id = $1 AND m.user_id = $2
ORDER BY p.name, p.id;

-- name: RenameProject :one
UPDATE project SET name = $3
WHERE org_id = $1 AND id = $2
RETURNING *;

-- name: DeleteProject :execrows
DELETE FROM project WHERE org_id = $1 AND id = $2;

-- name: CountProjectRuns :one
SELECT count(*) FROM run WHERE org_id = $1 AND project_id = $2;

-- name: GetMembership :one
SELECT * FROM membership
WHERE org_id = $1 AND user_id = $2 AND project_id = $3;

-- name: ListMembershipsOfMember :many
SELECT * FROM membership
WHERE org_id = $1 AND user_id = $2
ORDER BY project_id;

-- name: ListProjectMembers :many
SELECT m.*, u.email, u.name AS display_name
FROM membership m
JOIN app_user u ON u.org_id = m.org_id AND u.id = m.user_id
WHERE m.org_id = $1 AND m.project_id = $2
ORDER BY u.email;

-- Only the role is written. membership.permissions is never set by Ballast, and
-- is deliberately left untouched here: a mask somebody stored by hand survives
-- a role change, because an exception written by hand is meant to hold.
-- name: UpsertMembership :exec
INSERT INTO membership (org_id, user_id, project_id, role)
VALUES ($1, $2, $3, $4)
ON CONFLICT (org_id, user_id, project_id) DO UPDATE SET role = EXCLUDED.role;

-- name: DeleteMembership :execrows
DELETE FROM membership
WHERE org_id = $1 AND user_id = $2 AND project_id = $3;

-- name: GetUser :one
SELECT * FROM app_user WHERE org_id = $1 AND id = $2;
