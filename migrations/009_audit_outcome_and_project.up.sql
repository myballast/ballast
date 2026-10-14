BEGIN;

-- What the attempt came to.
--
-- The table recorded who did what to which target, and when — but not whether
-- it was allowed. A journal that cannot answer "show me every refusal this
-- month" answers the first question anybody asks it with a full table scan and
-- a list of action names somebody has to keep up to date.
--
-- A column rather than a convention in the action name: a CHECK constraint
-- cannot be forgotten, and an entry without an outcome cannot be written.
ALTER TABLE audit_log
    ADD COLUMN outcome text NOT NULL DEFAULT 'allowed'
        CHECK (outcome IN ('allowed', 'refused'));

-- Which project the action concerned.
--
-- Nullable on purpose: an emergency stop is an action on the whole
-- organization and belongs to no single project. Those entries are read by
-- every project's journal — an event that concerns the organization concerns
-- each of its projects, and one entry is written rather than one per project.
--
-- No foreign key to project: a project that is deleted takes its rows with it
-- through ON DELETE CASCADE everywhere else, and that is precisely wrong here.
-- The record of who deleted it, and of what was refused in it, has to outlive
-- it — otherwise deleting a project would be a way of erasing its trail.
ALTER TABLE audit_log ADD COLUMN project_id uuid;

-- The reading: one project's journal, newest first. The org-wide index from
-- migration 001 stays, for the day an organization-wide reading exists.
CREATE INDEX audit_log_org_project_created_idx
    ON audit_log (org_id, project_id, created_at DESC);

-- An entry outlives the person who caused it.
--
-- The intent was already written in migration 001 — actor_id is nullable, "null
-- when the actor is Ballast itself" — and ON DELETE SET NULL was meant to blank
-- it when somebody is removed. On a composite foreign key that sets *every*
-- column of the key, including org_id, which is NOT NULL: the referential
-- action could never run, so removing a person who left an entry failed
-- outright. An audit entry that keeps a user row alive is a journal that stops
-- an organization from removing somebody.
--
-- PostgreSQL 15 added the column list, which says which column to blank.
ALTER TABLE audit_log DROP CONSTRAINT audit_log_org_id_actor_id_fkey;
ALTER TABLE audit_log
    ADD CONSTRAINT audit_log_org_id_actor_id_fkey
    FOREIGN KEY (org_id, actor_id) REFERENCES app_user (org_id, id)
    ON DELETE SET NULL (actor_id);

-- Append-only, enforced by the database rather than by discipline.
--
-- An entry that can be rewritten is not evidence of anything: whoever wanted to
-- hide an approval would edit it rather than delete it. The application role
-- keeps INSERT and SELECT and loses the rest; migrations still run as the
-- owner, so this file can be rolled back.
--
-- Guarded on the role existing, because the name comes from the deployment
-- rather than from the schema: deploy/postgres/init creates it here.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'ballast_app') THEN
        REVOKE UPDATE, DELETE ON audit_log FROM ballast_app;
    END IF;
END
$$;

COMMIT;
