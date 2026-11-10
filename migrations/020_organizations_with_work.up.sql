-- A worker that serves every organization has to ask two questions that
-- row-level security, by design, refuses: which organizations have work, and
-- which organizations exist. Neither can be answered from a table the
-- application role can read, because an organization is exactly what is not
-- known yet. auth_lookup (migration 003) is the precedent, and these two are
-- kept as narrow as it is:
--
-- - Each returns one column, org_id. No name, no status, no count of runs: the
--   worker learns that an organization exists or has something in flight, and
--   nothing about it.
-- - They run as ballast_scheduler, a role nobody can sign in as, which reads
--   run(org_id, status) and organization(id) and has no write privilege at
--   all. It needs BYPASSRLS for the reason ballast_auth does: FORCE ROW LEVEL
--   SECURITY applies to every role not exempted, the function's owner included.
-- - The search_path is pinned, so a caller cannot slip in an object of its own
--   and have it run with the owner's rights.
-- - Only the application role may call them.
--
-- "Work" is a run that is queued, running or orphaned: the three states a worker
-- has to look at, to claim, to sweep around, or to collect.

BEGIN;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'ballast_scheduler') THEN
        CREATE ROLE ballast_scheduler NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE BYPASSRLS;
    END IF;
END
$$;

GRANT USAGE ON SCHEMA public TO ballast_scheduler;
GRANT SELECT (org_id, status) ON run TO ballast_scheduler;
GRANT SELECT (id) ON organization TO ballast_scheduler;

-- Keeps the poll cheap: it asks every second, and finished runs are the vast
-- majority of the table.
CREATE INDEX run_org_live_idx ON run (org_id)
    WHERE status IN ('queued', 'running', 'orphaned');

CREATE FUNCTION organizations_with_work()
RETURNS TABLE (org_id uuid)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, public
AS $$
    SELECT DISTINCT r.org_id
    FROM public.run r
    WHERE r.status IN ('queued', 'running', 'orphaned')
    ORDER BY r.org_id
$$;

CREATE FUNCTION organization_ids()
RETURNS TABLE (org_id uuid)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, public
AS $$
    SELECT o.id
    FROM public.organization o
    ORDER BY o.id
$$;

ALTER FUNCTION organizations_with_work() OWNER TO ballast_scheduler;
ALTER FUNCTION organization_ids() OWNER TO ballast_scheduler;

REVOKE ALL ON FUNCTION organizations_with_work() FROM PUBLIC;
REVOKE ALL ON FUNCTION organization_ids() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION organizations_with_work() TO ballast_app;
GRANT EXECUTE ON FUNCTION organization_ids() TO ballast_app;

COMMIT;
