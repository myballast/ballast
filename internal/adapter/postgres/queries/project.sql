-- Every statement runs inside InTenantTx, so row-level security already
-- restricts it to one organization. org_id is named in each WHERE anyway: it is
-- the first column of every index, and a policy is a safety net rather than a
-- query plan.

-- created_at comes from the injected clock rather than now(), so that what a
-- project records about when it was created can be controlled in a test.
-- name: InsertProject :one
INSERT INTO project (id, org_id, name, slug, infra, region, owner, stack, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
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

-- The mutable half of a project: its display name and what is said about it.
-- The slug is not here and never moves.
-- name: UpdateProject :one
UPDATE project SET name = $3, infra = $4, region = $5, owner = $6, stack = $7
WHERE org_id = $1 AND id = $2
RETURNING *;

-- The projects somebody keeps at the top. Both columns are named: row-level
-- security keeps another organization out and says nothing about another person,
-- so the user is in every one of these statements.
-- name: ListFavoriteProjects :many
SELECT project_id FROM project_favorite
WHERE org_id = $1 AND user_id = $2;

-- name: IsFavoriteProject :one
SELECT EXISTS (
    SELECT 1 FROM project_favorite
    WHERE org_id = $1 AND user_id = $2 AND project_id = $3
);

-- Starring twice is not an error, and not a second row.
-- name: AddFavoriteProject :exec
INSERT INTO project_favorite (org_id, user_id, project_id)
VALUES ($1, $2, $3)
ON CONFLICT DO NOTHING;

-- name: RemoveFavoriteProject :exec
DELETE FROM project_favorite
WHERE org_id = $1 AND user_id = $2 AND project_id = $3;

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

-- last_activity is the newest entry of the project's journal written by the
-- member. A left join, so that someone who never did anything has none.
-- name: ListProjectMembers :many
SELECT m.*, u.email, u.name AS display_name, act.last_at AS last_activity
FROM membership m
JOIN app_user u ON u.org_id = m.org_id AND u.id = m.user_id
LEFT JOIN (
    SELECT al.actor_id, max(al.created_at) AS last_at
    FROM audit_log al
    WHERE al.org_id = $1 AND al.project_id = $2 AND al.actor_id IS NOT NULL
    GROUP BY al.actor_id
) act ON act.actor_id = m.user_id
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

-- Every project of the organization, members or not.
--
-- Read by the emergency stop, which has to ask the authorizer about each one:
-- stopping an organization's runs is only an administrator's to do if they
-- administer all of it, and a project they are not even a member of is one they
-- do not administer.
-- name: ListProjectIDs :many
SELECT id FROM project WHERE org_id = $1 ORDER BY id;
