-- Invitations: a promise made to somebody who is not a member yet.
--
-- The link carries a random token; only its SHA-256 is stored, so a copy of this
-- table is not a set of working links. An invitation names one project and one
-- role, is good for one use, and expires.
--
-- Following the link crosses tenants, as signing in does. The person is signed in
-- to an organization of their own, or to none, and the one that invited them is
-- what they do not know yet. invitation_lookup is the opening, kept as narrow as
-- auth_lookup is (see 003), with a role of its own: ballast_auth is bounded to a
-- single table by a test, and widening it would be loosening that test.

BEGIN;

CREATE TABLE invitation (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      uuid NOT NULL REFERENCES organization (id) ON DELETE CASCADE,
    project_id  uuid NOT NULL,
    -- Kept in lower case so that the same person cannot be invited twice under
    -- two spellings.
    email       text NOT NULL CHECK (email = lower(email)),
    role        text NOT NULL CHECK (role IN ('viewer', 'runner', 'maintainer', 'admin')),
    -- Globally unique: the lookup finds an invitation by this alone.
    token_hash  text NOT NULL UNIQUE,
    -- Who invited. Cleared, not cascaded, when that person leaves: the invitation
    -- outlives its author.
    invited_by  uuid,
    expires_at  timestamptz NOT NULL,
    accepted_at timestamptz,
    revoked_at  timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),

    UNIQUE (org_id, id),
    FOREIGN KEY (org_id, project_id) REFERENCES project (org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, invited_by) REFERENCES app_user (org_id, id) ON DELETE SET NULL (invited_by)
);

-- One pending invitation per person and project: two would be two working links,
-- and a revocation that leaves one of them open.
CREATE UNIQUE INDEX invitation_pending_idx
    ON invitation (org_id, project_id, email)
    WHERE accepted_at IS NULL AND revoked_at IS NULL;

CREATE INDEX invitation_org_project_idx ON invitation (org_id, project_id, created_at DESC);

ALTER TABLE invitation ENABLE ROW LEVEL SECURITY;
ALTER TABLE invitation FORCE ROW LEVEL SECURITY;

CREATE POLICY invitation_tenant_isolation ON invitation
    USING (org_id = nullif(current_setting('app.current_org', true), '')::uuid)
    WITH CHECK (org_id = nullif(current_setting('app.current_org', true), '')::uuid);

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'ballast_invite') THEN
        CREATE ROLE ballast_invite NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE BYPASSRLS;
    END IF;
END
$$;

GRANT USAGE ON SCHEMA public TO ballast_invite;
GRANT SELECT (id, org_id, project_id, email, role, token_hash, expires_at, accepted_at, revoked_at)
    ON invitation TO ballast_invite;
GRANT SELECT (id, org_id, name) ON project TO ballast_invite;
GRANT SELECT (id, name) ON organization TO ballast_invite;

-- The state is returned whatever it is: deciding that a used, revoked or expired
-- link is dead belongs to the caller, which has the clock.
CREATE FUNCTION invitation_lookup(p_token_hash text)
RETURNS TABLE (
    invitation_id uuid,
    org_id        uuid,
    project_id    uuid,
    email         text,
    role          text,
    expires_at    timestamptz,
    accepted_at   timestamptz,
    revoked_at    timestamptz,
    project_name  text,
    org_name      text
)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, public
AS $$
    SELECT i.id, i.org_id, i.project_id, i.email, i.role, i.expires_at,
           i.accepted_at, i.revoked_at, p.name, o.name
    FROM public.invitation i
    JOIN public.project p ON p.org_id = i.org_id AND p.id = i.project_id
    JOIN public.organization o ON o.id = i.org_id
    WHERE i.token_hash = p_token_hash
$$;

ALTER FUNCTION invitation_lookup(text) OWNER TO ballast_invite;

REVOKE ALL ON FUNCTION invitation_lookup(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION invitation_lookup(text) TO ballast_app;

COMMIT;
