-- Reverses 003_auth_lookup.up.sql.
--
-- Roles belong to the cluster, not to this database, so ballast_auth is dropped
-- only once nothing here refers to it any more.

BEGIN;

DROP FUNCTION auth_lookup(text);

REVOKE SELECT (id, org_id, subject, created_at) ON app_user FROM ballast_auth;
REVOKE USAGE ON SCHEMA public FROM ballast_auth;

DROP ROLE ballast_auth;

COMMIT;
