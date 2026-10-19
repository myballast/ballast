-- What the projects page shows beside a name, and which projects somebody keeps
-- at the top.
--
-- Details are optional text a person writes: where the project runs (infra,
-- region), who answers for it (owner), and short labels for what it is made of
-- (stack). Empty strings and an empty array mean "not said", and the screens
-- leave them out rather than showing a blank. The bounds are here as well as in
-- the domain, so that no other writer can store a paragraph.
--
-- A favourite belongs to one person in one project. Row-level security isolates
-- organizations and says nothing about people, so what keeps one person's stars
-- from another is that every statement names user_id, and a test.

BEGIN;

ALTER TABLE project
    ADD COLUMN infra  text   NOT NULL DEFAULT '',
    ADD COLUMN region text   NOT NULL DEFAULT '',
    ADD COLUMN owner  text   NOT NULL DEFAULT '',
    ADD COLUMN stack  text[] NOT NULL DEFAULT '{}';

ALTER TABLE project
    ADD CONSTRAINT project_infra_length  CHECK (char_length(infra) <= 40),
    ADD CONSTRAINT project_region_length CHECK (char_length(region) <= 40),
    ADD CONSTRAINT project_owner_length  CHECK (char_length(owner) <= 80),
    ADD CONSTRAINT project_stack_size    CHECK (cardinality(stack) <= 12);

CREATE TABLE project_favorite (
    org_id     uuid NOT NULL REFERENCES organization (id) ON DELETE CASCADE,
    user_id    uuid NOT NULL,
    project_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (org_id, user_id, project_id),
    FOREIGN KEY (org_id, user_id)    REFERENCES app_user (org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, project_id) REFERENCES project (org_id, id)  ON DELETE CASCADE
);

-- Migration 001 walks the catalogue to apply row-level security, which covers the
-- tables that existed then. A table added later writes its own.
ALTER TABLE project_favorite ENABLE ROW LEVEL SECURITY;
ALTER TABLE project_favorite FORCE ROW LEVEL SECURITY;

CREATE POLICY project_favorite_tenant_isolation ON project_favorite
    USING (org_id = nullif(current_setting('app.current_org', true), '')::uuid)
    WITH CHECK (org_id = nullif(current_setting('app.current_org', true), '')::uuid);

COMMIT;
