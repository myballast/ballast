-- A first sign-in creates an organization and everything its first user needs,
-- in one tenant transaction scoped to the organization being created. The
-- policies accept it: organization.org_id is generated from the row's own id,
-- which is the organization announced to the transaction.

-- name: CreateOrganization :exec
INSERT INTO organization (id, name, created_at) VALUES ($1, $2, $3);

-- name: CreateUser :exec
INSERT INTO app_user (id, org_id, subject, email, name, created_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: CreateProject :exec
INSERT INTO project (id, org_id, name, slug, created_at) VALUES ($1, $2, $3, $4, $5);

-- name: CreateMembership :exec
INSERT INTO membership (org_id, user_id, project_id, role, created_at)
VALUES ($1, $2, $3, $4, $5);
