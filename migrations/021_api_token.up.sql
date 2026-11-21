-- API tokens: the credential a pipeline holds instead of a session.
--
-- A token names one project and carries two permissions, fixed in the code
-- (view and run). Only its SHA-256 is stored, so a copy of this table is not a
-- set of working credentials. Every token expires: the default lifetime is
-- applied by the application, and the database refuses a row written without
-- one.
--
-- A request that carries a token knows neither the organization nor the project
-- it belongs to; that is exactly what the token says, and row-level security
-- refuses a read that does not name an organization. api_token_lookup is the
-- opening, kept as narrow as auth_lookup (003) and invitation_lookup (013) are,
-- with a role of its own: those two are each bounded to their tables by a test,
-- and widening one would be loosening it.

BEGIN;

CREATE TABLE api_token (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id       uuid NOT NULL REFERENCES organization (id) ON DELETE CASCADE,
    project_id   uuid NOT NULL,
    name         text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 64),
    -- What the list shows so that two tokens can be told apart: the readable
    -- prefix and a few characters. Not enough to rebuild or search for the secret.
    prefix       text NOT NULL,
    -- Globally unique: the lookup finds a token by this alone.
    token_hash   text NOT NULL UNIQUE,
    -- Who created it. Cleared, not cascaded, when that person leaves: a pipeline
    -- must not stop because somebody did.
    created_by   uuid,
    expires_at   timestamptz NOT NULL,
    last_used_at timestamptz,
    revoked_at   timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),

    UNIQUE (org_id, id),
    FOREIGN KEY (org_id, project_id) REFERENCES project (org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, created_by) REFERENCES app_user (org_id, id) ON DELETE SET NULL (created_by)
);

CREATE INDEX api_token_org_project_idx ON api_token (org_id, project_id, created_at DESC);

ALTER TABLE api_token ENABLE ROW LEVEL SECURITY;
ALTER TABLE api_token FORCE ROW LEVEL SECURITY;

CREATE POLICY api_token_tenant_isolation ON api_token
    USING (org_id = nullif(current_setting('app.current_org', true), '')::uuid)
    WITH CHECK (org_id = nullif(current_setting('app.current_org', true), '')::uuid);

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'ballast_token') THEN
        CREATE ROLE ballast_token NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE BYPASSRLS;
    END IF;
END
$$;

-- Six columns of one table, nothing else. Not the name, the prefix, who created
-- it or when it was last used: those are what a tenant knows about its own
-- tokens, and nothing the lookup returns.
GRANT USAGE ON SCHEMA public TO ballast_token;
GRANT SELECT (id, org_id, project_id, token_hash, expires_at, revoked_at)
    ON api_token TO ballast_token;

-- The state is returned whatever it is: deciding that a revoked or expired token
-- is dead belongs to the caller, which has the clock.
CREATE FUNCTION api_token_lookup(p_token_hash text)
RETURNS TABLE (
    token_id   uuid,
    org_id     uuid,
    project_id uuid,
    expires_at timestamptz,
    revoked_at timestamptz
)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, public
AS $$
    SELECT t.id, t.org_id, t.project_id, t.expires_at, t.revoked_at
    FROM public.api_token t
    WHERE t.token_hash = p_token_hash
$$;

ALTER FUNCTION api_token_lookup(text) OWNER TO ballast_token;

REVOKE ALL ON FUNCTION api_token_lookup(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION api_token_lookup(text) TO ballast_app;

COMMIT;
