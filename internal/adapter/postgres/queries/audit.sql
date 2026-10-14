-- Every statement here runs inside InTenantTx, so row-level security already
-- restricts it to one organization.

-- One line of the journal.
--
-- Nothing is ever updated or deleted here: an audit entry that can be rewritten
-- is not evidence of anything. The only thing that removes a row is the
-- organization itself being deleted.
-- name: RecordAudit :one
INSERT INTO audit_log (
    org_id, actor_id, project_id, action, outcome, target_type, target_id, metadata, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- One project's journal, newest first.
--
-- Entries with no project are included, and that is deliberate: an action on
-- the whole organization — an emergency stop — concerns every project in it. It
-- is written once and read from each project's journal, rather than duplicated
-- per project.
--
-- Limited, because a journal only grows.
-- name: ListProjectAudit :many
SELECT * FROM audit_log
WHERE org_id = $1
  AND (project_id = $2 OR project_id IS NULL)
ORDER BY created_at DESC, id DESC
LIMIT $3;
