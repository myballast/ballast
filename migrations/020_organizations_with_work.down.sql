-- Reverses 020_organizations_with_work.up.sql. The role is dropped last, once
-- nothing it owns or may read remains.

BEGIN;

DROP FUNCTION IF EXISTS organizations_with_work();
DROP FUNCTION IF EXISTS organization_ids();
DROP INDEX IF EXISTS run_org_live_idx;

REVOKE SELECT (org_id, status) ON run FROM ballast_scheduler;
REVOKE SELECT (id) ON organization FROM ballast_scheduler;
REVOKE USAGE ON SCHEMA public FROM ballast_scheduler;

DROP ROLE ballast_scheduler;

COMMIT;
