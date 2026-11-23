-- Every statement here runs inside InTenantTx, so row-level security already
-- restricts it to one organization.

-- The webhook of a project, without its secret: what the screen and the authorizer
-- read. The sealed column is not selected, so that nothing that reads a row for its
-- dates can carry the secret along.
-- name: GetProjectWebhook :one
SELECT id, org_id, project_id, created_by, created_at, rotated_at
FROM project_webhook
WHERE org_id = $1 AND project_id = $2;

-- name: GetWebhookByID :one
SELECT id, org_id, project_id, created_by, created_at, rotated_at
FROM project_webhook
WHERE org_id = $1 AND id = $2;

-- The one query that returns the sealed secret, for the one caller that verifies a
-- signature.
-- name: GetWebhookSecret :one
SELECT id, org_id, project_id, created_by, created_at, rotated_at, secret_encrypted
FROM project_webhook
WHERE org_id = $1 AND project_id = $2;

-- name: InsertWebhook :one
INSERT INTO project_webhook (id, org_id, project_id, secret_encrypted, created_by, created_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, org_id, project_id, created_by, created_at, rotated_at;

-- name: RotateWebhookSecret :execrows
UPDATE project_webhook
SET secret_encrypted = $3, rotated_at = $4
WHERE org_id = $1 AND id = $2;

-- name: DeleteWebhook :execrows
DELETE FROM project_webhook WHERE org_id = $1 AND id = $2;

-- A delivery served. The key decides, not a read before the write: two requests with
-- the same identifier arriving together cannot both find it absent.
-- name: RememberWebhookDelivery :execrows
INSERT INTO webhook_delivery (org_id, project_id, delivery_id, received_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (org_id, project_id, delivery_id) DO NOTHING;

-- What is past the retention. Read by date, within an organization.
-- name: ForgetWebhookDeliveries :execrows
DELETE FROM webhook_delivery WHERE org_id = $1 AND received_at < $2;
