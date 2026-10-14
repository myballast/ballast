BEGIN;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'ballast_app') THEN
        GRANT UPDATE, DELETE ON audit_log TO ballast_app;
    END IF;
END
$$;

ALTER TABLE audit_log DROP CONSTRAINT IF EXISTS audit_log_org_id_actor_id_fkey;
ALTER TABLE audit_log
    ADD CONSTRAINT audit_log_org_id_actor_id_fkey
    FOREIGN KEY (org_id, actor_id) REFERENCES app_user (org_id, id)
    ON DELETE SET NULL;

DROP INDEX IF EXISTS audit_log_org_project_created_idx;

ALTER TABLE audit_log DROP COLUMN IF EXISTS project_id;
ALTER TABLE audit_log DROP COLUMN IF EXISTS outcome;

COMMIT;
