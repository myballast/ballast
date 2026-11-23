-- Reverses 024_project_webhook.up.sql.

BEGIN;

ALTER TABLE audit_log DROP CONSTRAINT IF EXISTS audit_log_actor_one_kind;
ALTER TABLE audit_log DROP COLUMN IF EXISTS actor_webhook_id;
ALTER TABLE audit_log
    ADD CONSTRAINT audit_log_actor_one_kind CHECK (actor_id IS NULL OR actor_token_id IS NULL);

DROP INDEX IF EXISTS run_org_webhook_created_idx;
ALTER TABLE run DROP CONSTRAINT IF EXISTS run_triggered_by_one_kind;
ALTER TABLE run DROP COLUMN IF EXISTS triggered_by_webhook;
ALTER TABLE run
    ADD CONSTRAINT run_triggered_by_one_kind CHECK (triggered_by IS NULL OR triggered_by_token IS NULL);

DROP TABLE IF EXISTS webhook_delivery;
DROP TABLE IF EXISTS project_webhook;

COMMIT;
