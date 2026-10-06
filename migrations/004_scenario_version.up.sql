-- A run freezes the scenario version it executed. Until now that number pointed
-- at nothing: scenario held one script column, so editing a scenario overwrote
-- what every past run had run. The number survived and its meaning did not.
--
-- scenario_version keeps every version. Editing a scenario inserts a row and
-- rewrites nothing, and run gains a foreign key into it, so "which script did
-- this run execute" is answered by the schema rather than by convention.
--
-- scenario keeps its script, version, default_profile and default_profile_params
-- as the current version, denormalised: the worker and the existing reads use
-- them, and a join on every run would buy nothing. The repository writes both in
-- one transaction, and an integration test holds them to each other.

BEGIN;

CREATE TABLE scenario_version (
    org_id                 uuid NOT NULL REFERENCES organization (id) ON DELETE CASCADE,
    scenario_id            uuid NOT NULL,
    version                integer NOT NULL CHECK (version > 0),
    script                 text NOT NULL,
    default_profile        text NOT NULL
        CHECK (default_profile IN ('smoke', 'load', 'stress', 'spike', 'soak')),
    default_profile_params jsonb NOT NULL DEFAULT '{}'::jsonb,
    -- Null when the person who wrote this version has since been removed.
    created_by             uuid,
    created_at             timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (org_id, scenario_id, version),
    FOREIGN KEY (org_id, scenario_id) REFERENCES scenario (org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, created_by) REFERENCES app_user (org_id, id) ON DELETE SET NULL
);

-- Every scenario that already exists becomes its own first version. Its current
-- script is the only one anybody still has.
INSERT INTO scenario_version
    (org_id, scenario_id, version, script, default_profile, default_profile_params, created_at)
SELECT org_id, id, version, script, default_profile, default_profile_params, created_at
FROM scenario;

-- A run that references a version with no row would fail here, and should: it
-- would mean the database holds a run whose script nothing can account for, and
-- inventing one to make the constraint pass would be worse than stopping.
ALTER TABLE run ADD CONSTRAINT run_scenario_version_fkey
    FOREIGN KEY (org_id, scenario_id, scenario_version)
    REFERENCES scenario_version (org_id, scenario_id, version);

CREATE INDEX scenario_version_org_scenario_idx
    ON scenario_version (org_id, scenario_id, version DESC);

ALTER TABLE scenario_version ENABLE ROW LEVEL SECURITY;
ALTER TABLE scenario_version FORCE ROW LEVEL SECURITY;

CREATE POLICY scenario_version_tenant_isolation ON scenario_version
    USING (org_id = nullif(current_setting('app.current_org', true), '')::uuid)
    WITH CHECK (org_id = nullif(current_setting('app.current_org', true), '')::uuid);

COMMIT;
