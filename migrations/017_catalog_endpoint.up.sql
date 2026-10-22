-- The endpoints a project knows about, read from the specifications somebody
-- imported.
--
-- An import used to consume its file: a scenario came out of it and the file was
-- forgotten. The catalog keeps what the file said, so that a scenario written
-- later -- in code or on the canvas -- can pick an endpoint instead of typing it.
--
-- It holds what a graph node would hold, and nothing a graph node may not: no
-- credential header, no body guessed from a schema. The bounds are here as well
-- as in the domain, so that no other writer can store a paragraph.
--
-- source says which file an endpoint came from. Importing the same source again
-- replaces what that source had, and the unique key below is what makes a
-- second copy of the same endpoint impossible rather than merely unlikely.

BEGIN;

CREATE TABLE catalog_endpoint (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id        uuid NOT NULL REFERENCES organization (id) ON DELETE CASCADE,
    project_id    uuid NOT NULL,
    source        text NOT NULL CHECK (char_length(source) BETWEEN 1 AND 120),
    method        text NOT NULL CHECK (method IN ('GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD')),
    -- A template, as the specification wrote it: /users/{id}.
    path          text NOT NULL CHECK (path LIKE '/%' AND char_length(path) <= 2000),
    tag           text NOT NULL DEFAULT '' CHECK (char_length(tag) <= 120),
    summary       text NOT NULL DEFAULT '' CHECK (char_length(summary) <= 200),
    path_params   text[] NOT NULL DEFAULT '{}' CHECK (cardinality(path_params) <= 50),
    headers       jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(headers) = 'object'),
    content_type  text NOT NULL DEFAULT '' CHECK (char_length(content_type) <= 200),
    body          text NOT NULL DEFAULT '' CHECK (char_length(body) <= 2000),
    body_too_long boolean NOT NULL DEFAULT false,
    imported_by   uuid,
    created_at    timestamptz NOT NULL DEFAULT now(),

    UNIQUE (org_id, project_id, source, method, path),
    FOREIGN KEY (org_id, project_id) REFERENCES project (org_id, id) ON DELETE CASCADE,
    -- An endpoint outlives the person who imported it. The column list blanks
    -- imported_by alone: on a composite key the default would try to blank
    -- org_id too, which is NOT NULL, and removing the person would fail.
    FOREIGN KEY (org_id, imported_by) REFERENCES app_user (org_id, id) ON DELETE SET NULL (imported_by)
);

-- The list the pickers read: one project's endpoints, by tag then path.
CREATE INDEX catalog_endpoint_project_idx ON catalog_endpoint (org_id, project_id, tag, path);

-- Migration 001 walks the catalogue to apply row-level security, which covers the
-- tables that existed then. A table added later writes its own.
ALTER TABLE catalog_endpoint ENABLE ROW LEVEL SECURITY;
ALTER TABLE catalog_endpoint FORCE ROW LEVEL SECURITY;

CREATE POLICY catalog_endpoint_tenant_isolation ON catalog_endpoint
    USING (org_id = nullif(current_setting('app.current_org', true), '')::uuid)
    WITH CHECK (org_id = nullif(current_setting('app.current_org', true), '')::uuid);

COMMIT;
