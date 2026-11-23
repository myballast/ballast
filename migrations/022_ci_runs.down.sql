-- Reverses 022_ci_runs.up.sql.

BEGIN;

ALTER TABLE audit_log DROP CONSTRAINT IF EXISTS audit_log_actor_one_kind;
ALTER TABLE audit_log DROP COLUMN IF EXISTS actor_token_id;

DROP INDEX IF EXISTS run_org_token_created_idx;
ALTER TABLE run DROP CONSTRAINT IF EXISTS run_triggered_by_one_kind;
ALTER TABLE run DROP CONSTRAINT IF EXISTS run_org_id_triggered_by_token_fkey;
ALTER TABLE run DROP COLUMN IF EXISTS triggered_by_token;

COMMIT;
