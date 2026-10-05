-- Sign-in needs one answer that row-level security, by design, refuses: which
-- organization does this identity belong to? The organization is precisely
-- what is not known yet, and the application role sees no row without it.
--
-- auth_lookup is the one opening, and it is kept as narrow as it can be:
--
-- - It answers a single question, by exact match on the subject, with two
--   identifiers. No email, no name, nothing to learn about a tenant from.
-- - It runs as ballast_auth, a role nobody can sign in as, which may read four
--   columns of app_user and nothing else in the schema. It needs BYPASSRLS
--   because FORCE ROW LEVEL SECURITY applies to every role that is not
--   exempted, the function's owner included; owned by the schema owner instead,
--   it would only work where that owner happens to be a superuser.
-- - Its search_path is pinned, so a caller cannot slip in an object of its own
--   and have it run with the owner's rights.
-- - Only the application role may call it.
--
-- ballast_app itself stays NOBYPASSRLS. Creating a BYPASSRLS role requires the
-- migration to run as a superuser.
--
-- Every match is returned, oldest first. A subject belongs to one organization
-- until invitations exist; when it belongs to several, choosing between them is
-- the application's decision, not something to settle silently here.

BEGIN;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'ballast_auth') THEN
        CREATE ROLE ballast_auth NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE BYPASSRLS;
    END IF;
END
$$;

GRANT USAGE ON SCHEMA public TO ballast_auth;
GRANT SELECT (id, org_id, subject, created_at) ON app_user TO ballast_auth;

CREATE FUNCTION auth_lookup(p_subject text)
RETURNS TABLE (org_id uuid, user_id uuid)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, public
AS $$
    SELECT u.org_id, u.id
    FROM public.app_user u
    WHERE u.subject = p_subject
    ORDER BY u.created_at, u.id
$$;

ALTER FUNCTION auth_lookup(text) OWNER TO ballast_auth;

REVOKE ALL ON FUNCTION auth_lookup(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION auth_lookup(text) TO ballast_app;

COMMIT;
