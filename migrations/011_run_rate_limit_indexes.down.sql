BEGIN;

DROP INDEX IF EXISTS run_org_queued_idx;
DROP INDEX IF EXISTS run_org_created_idx;

COMMIT;
