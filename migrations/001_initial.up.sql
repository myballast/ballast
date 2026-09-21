-- Migration 001 — initial schema.
--
-- Two rules shape every table below, and neither is optional.
--
-- 1. org_id is present everywhere, even where a join could derive it. That is
--    what lets a single Row-Level Security policy protect a table without
--    joining anything.
-- 2. References between tables carry org_id as part of the foreign key, so a
--    row can only ever point at a row of its own organization. Row-Level
--    Security does not cover that on its own: it filters what a statement can
--    see, not what an id is allowed to mean.
--
-- Reserved and deliberately unused in lot 1: environment.runner_id,
-- membership.permissions, run.schedule_id.

BEGIN;

-- ---------------------------------------------------------------------------
-- Tenancy
-- ---------------------------------------------------------------------------

CREATE TABLE organization (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    -- Every table carries org_id, this one included, where it is the row's own
    -- id. Generated rather than written by hand so that the policy, the index
    -- and the tests treat all tables alike, with no exception to remember.
    org_id     uuid NOT NULL GENERATED ALWAYS AS (id) STORED,
    name       text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),

    UNIQUE (org_id)
);

CREATE TABLE app_user (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id     uuid NOT NULL REFERENCES organization (id) ON DELETE CASCADE,
    -- Subject claim of the OIDC provider. Ballast stores no password.
    subject    text NOT NULL,
    email      text NOT NULL,
    name       text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),

    UNIQUE (org_id, id),
    UNIQUE (org_id, subject),
    UNIQUE (org_id, email)
);

CREATE TABLE project (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id     uuid NOT NULL REFERENCES organization (id) ON DELETE CASCADE,
    name       text NOT NULL,
    slug       text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),

    UNIQUE (org_id, id),
    UNIQUE (org_id, slug)
);

-- Hosts a project is allowed to send traffic to. Filled when an admin approves
-- a verified environment; empty until then.
CREATE TABLE project_allowed_host (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id     uuid NOT NULL REFERENCES organization (id) ON DELETE CASCADE,
    project_id uuid NOT NULL,
    host       text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),

    FOREIGN KEY (org_id, project_id) REFERENCES project (org_id, id) ON DELETE CASCADE,
    UNIQUE (org_id, project_id, host)
);

CREATE TABLE membership (
    org_id      uuid NOT NULL REFERENCES organization (id) ON DELETE CASCADE,
    user_id     uuid NOT NULL,
    project_id  uuid NOT NULL,
    role        text NOT NULL CHECK (role IN ('viewer', 'runner', 'maintainer', 'admin')),
    -- Reserved. The permission mask is derived from the role in Go; this column
    -- is for a future per-membership override and stays null in lot 1.
    permissions bigint,
    created_at  timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (org_id, user_id, project_id),
    FOREIGN KEY (org_id, user_id) REFERENCES app_user (org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, project_id) REFERENCES project (org_id, id) ON DELETE CASCADE
);

-- ---------------------------------------------------------------------------
-- Targets
-- ---------------------------------------------------------------------------

CREATE TABLE environment (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id              uuid NOT NULL REFERENCES organization (id) ON DELETE CASCADE,
    project_id          uuid NOT NULL,
    name                text NOT NULL,
    base_url            text NOT NULL,
    verification_token  text NOT NULL,
    -- A valid proof and a human approval are two separate steps. verified_at
    -- alone cannot express "the proof was served but nobody approved it yet".
    verification_status text NOT NULL DEFAULT 'pending'
        CHECK (verification_status IN ('pending', 'proof_ok', 'verified', 'verification_failed')),
    verified_at         timestamptz,
    verified_by         uuid,
    max_vus             integer NOT NULL CHECK (max_vus > 0),
    max_duration_s      integer NOT NULL CHECK (max_duration_s > 0),
    -- Reserved for the client-side runner of phase 3. Null means Ballast's own
    -- generators.
    runner_id           uuid,
    created_at          timestamptz NOT NULL DEFAULT now(),

    UNIQUE (org_id, id),
    UNIQUE (org_id, project_id, name),
    FOREIGN KEY (org_id, project_id) REFERENCES project (org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, verified_by) REFERENCES app_user (org_id, id) ON DELETE SET NULL,
    -- An environment is verified only with both a timestamp and an approver.
    CHECK (
        (verification_status = 'verified') = (verified_at IS NOT NULL AND verified_by IS NOT NULL)
    )
);

CREATE TABLE secret (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          uuid NOT NULL REFERENCES organization (id) ON DELETE CASCADE,
    environment_id  uuid NOT NULL,
    key             text NOT NULL,
    -- Ciphertext. Nothing readable is ever written to this column.
    value_encrypted bytea NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),

    FOREIGN KEY (org_id, environment_id) REFERENCES environment (org_id, id) ON DELETE CASCADE,
    UNIQUE (org_id, environment_id, key)
);

-- ---------------------------------------------------------------------------
-- Scenarios
-- ---------------------------------------------------------------------------

CREATE TABLE scenario (
    id                     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id                 uuid NOT NULL REFERENCES organization (id) ON DELETE CASCADE,
    project_id             uuid NOT NULL,
    name                   text NOT NULL,
    script                 text NOT NULL,
    version                integer NOT NULL DEFAULT 1 CHECK (version > 0),
    default_profile        text NOT NULL DEFAULT 'smoke'
        CHECK (default_profile IN ('smoke', 'load', 'stress', 'spike', 'soak')),
    default_profile_params jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at             timestamptz NOT NULL DEFAULT now(),
    updated_at             timestamptz NOT NULL DEFAULT now(),

    UNIQUE (org_id, id),
    UNIQUE (org_id, project_id, name),
    FOREIGN KEY (org_id, project_id) REFERENCES project (org_id, id) ON DELETE CASCADE
);

CREATE TABLE threshold (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      uuid NOT NULL REFERENCES organization (id) ON DELETE CASCADE,
    scenario_id uuid NOT NULL,
    -- Accepts k6's tagged syntax, e.g. http_req_duration{name:POST /login},
    -- which is what allows one threshold per endpoint.
    metric      text NOT NULL,
    expression  text NOT NULL,

    FOREIGN KEY (org_id, scenario_id) REFERENCES scenario (org_id, id) ON DELETE CASCADE,
    UNIQUE (org_id, scenario_id, metric, expression)
);

-- ---------------------------------------------------------------------------
-- Runs
-- ---------------------------------------------------------------------------

CREATE TABLE run (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id           uuid NOT NULL REFERENCES organization (id) ON DELETE CASCADE,
    -- Derivable through scenario, denormalized for the same reason org_id is:
    -- listing a project's runs must not need a join to stay fast.
    project_id       uuid NOT NULL,
    scenario_id      uuid NOT NULL,
    environment_id   uuid NOT NULL,
    -- Reserved. Null means a manual trigger. No foreign key: the schedule table
    -- belongs to lot 3.
    schedule_id      uuid,
    -- Frozen at execution time. Editing a scenario must never rewrite history.
    scenario_version integer NOT NULL CHECK (scenario_version > 0),
    profile          text NOT NULL
        CHECK (profile IN ('smoke', 'load', 'stress', 'spike', 'soak')),
    profile_params   jsonb NOT NULL DEFAULT '{}'::jsonb,
    status           text NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued', 'running', 'passed', 'failed', 'errored',
                          'timeout', 'cancelled', 'rejected', 'skipped', 'orphaned')),
    triggered_by     uuid,
    egress_bytes     bigint NOT NULL DEFAULT 0 CHECK (egress_bytes >= 0),
    error_message    text,
    created_at       timestamptz NOT NULL DEFAULT now(),
    started_at       timestamptz,
    ended_at         timestamptz,
    heartbeat_at     timestamptz,

    UNIQUE (org_id, id),
    FOREIGN KEY (org_id, project_id) REFERENCES project (org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, scenario_id) REFERENCES scenario (org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, environment_id) REFERENCES environment (org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, triggered_by) REFERENCES app_user (org_id, id) ON DELETE SET NULL
);

CREATE TABLE run_metric (
    id       uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id   uuid NOT NULL REFERENCES organization (id) ON DELETE CASCADE,
    run_id   uuid NOT NULL,
    name     text NOT NULL,
    -- Empty string rather than null: a unique constraint does not deduplicate
    -- nulls, so a nullable tag would let the same metric be inserted twice.
    tag_name text NOT NULL DEFAULT '',
    value    double precision NOT NULL,

    FOREIGN KEY (org_id, run_id) REFERENCES run (org_id, id) ON DELETE CASCADE,
    UNIQUE (org_id, run_id, name, tag_name)
);

-- The verdict as k6 reported it. Ballast never recomputes it.
CREATE TABLE run_threshold (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id     uuid NOT NULL REFERENCES organization (id) ON DELETE CASCADE,
    run_id     uuid NOT NULL,
    metric     text NOT NULL,
    expression text NOT NULL,
    passed     boolean NOT NULL,

    FOREIGN KEY (org_id, run_id) REFERENCES run (org_id, id) ON DELETE CASCADE,
    UNIQUE (org_id, run_id, metric, expression)
);

CREATE TABLE run_artifact (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      uuid NOT NULL REFERENCES organization (id) ON DELETE CASCADE,
    run_id      uuid NOT NULL,
    kind        text NOT NULL,
    -- Object keys are tenant-prefixed: org/<org_id>/run/<run_id>/...
    storage_key text NOT NULL,
    size_bytes  bigint NOT NULL DEFAULT 0 CHECK (size_bytes >= 0),
    created_at  timestamptz NOT NULL DEFAULT now(),

    FOREIGN KEY (org_id, run_id) REFERENCES run (org_id, id) ON DELETE CASCADE,
    UNIQUE (org_id, run_id, kind)
);

CREATE TABLE run_log (
    id      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id  uuid NOT NULL REFERENCES organization (id) ON DELETE CASCADE,
    run_id  uuid NOT NULL,
    -- Line number within the run. Ordering, not an identifier, and scoped to
    -- one run so no sequence is shared across tenants.
    seq     bigint NOT NULL CHECK (seq >= 0),
    stream  text NOT NULL CHECK (stream IN ('stdout', 'stderr')),
    message text NOT NULL,
    logged_at timestamptz NOT NULL DEFAULT now(),

    FOREIGN KEY (org_id, run_id) REFERENCES run (org_id, id) ON DELETE CASCADE,
    UNIQUE (org_id, run_id, seq)
);

-- ---------------------------------------------------------------------------
-- Audit
-- ---------------------------------------------------------------------------

-- Who did what, on which target, when. Also records refusals: an attempt to run
-- against an unverified target is exactly what this table exists for.
CREATE TABLE audit_log (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      uuid NOT NULL REFERENCES organization (id) ON DELETE CASCADE,
    -- Null when the actor is Ballast itself rather than a person.
    actor_id    uuid,
    action      text NOT NULL,
    target_type text NOT NULL,
    target_id   uuid,
    -- Never a secret, a token, an Authorization header or a Cookie.
    metadata    jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at  timestamptz NOT NULL DEFAULT now(),

    FOREIGN KEY (org_id, actor_id) REFERENCES app_user (org_id, id) ON DELETE SET NULL
);

-- ---------------------------------------------------------------------------
-- Indexes
--
-- org_id comes first everywhere: it is the column every statement filters on.
-- Tables whose only access pattern is by primary key already get their org-first
-- index from a UNIQUE (org_id, id) constraint above.
-- ---------------------------------------------------------------------------

CREATE INDEX app_user_org_email_idx          ON app_user (org_id, email);
CREATE INDEX membership_org_project_idx      ON membership (org_id, project_id);
CREATE INDEX environment_org_project_idx     ON environment (org_id, project_id);
CREATE INDEX secret_org_environment_idx      ON secret (org_id, environment_id);
CREATE INDEX scenario_org_project_idx        ON scenario (org_id, project_id);
CREATE INDEX threshold_org_scenario_idx      ON threshold (org_id, scenario_id);
CREATE INDEX project_allowed_host_org_idx    ON project_allowed_host (org_id, project_id);

-- The run history screen: a project's runs, newest first.
CREATE INDEX run_org_project_created_idx     ON run (org_id, project_id, created_at DESC);
-- The trend of one scenario over its last N runs.
CREATE INDEX run_org_scenario_created_idx    ON run (org_id, scenario_id, created_at DESC);
-- The worker picking up work, and the one-run-per-environment rule.
CREATE INDEX run_org_environment_status_idx  ON run (org_id, environment_id, status);

CREATE INDEX run_metric_org_run_idx          ON run_metric (org_id, run_id);
CREATE INDEX run_threshold_org_run_idx       ON run_threshold (org_id, run_id);
CREATE INDEX run_artifact_org_run_idx        ON run_artifact (org_id, run_id);
CREATE INDEX run_log_org_run_seq_idx         ON run_log (org_id, run_id, seq);
CREATE INDEX audit_log_org_created_idx       ON audit_log (org_id, created_at DESC);

-- ---------------------------------------------------------------------------
-- Row-Level Security
--
-- Applied by walking the catalog rather than a list written here. A list is a
-- place to forget a table; the catalog cannot be out of date. This is also why
-- golang-migrate's bookkeeping table lives in its own schema.
--
-- ENABLE is not enough on its own: without FORCE, the role that owns the table
-- reads straight past every policy below.
-- ---------------------------------------------------------------------------

DO $$
DECLARE
    t text;
BEGIN
    FOR t IN
        SELECT tablename FROM pg_tables WHERE schemaname = 'public' ORDER BY tablename
    LOOP
        EXECUTE format('ALTER TABLE public.%I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format('ALTER TABLE public.%I FORCE ROW LEVEL SECURITY', t);

        -- Two guards, and both are needed.
        --
        -- current_setting's second argument is missing_ok: without it, a
        -- statement that never announced its organization raises
        -- "unrecognized configuration parameter" rather than simply seeing
        -- nothing.
        --
        -- nullif covers the case that actually happens in production. A
        -- set_config with a local scope does not return the parameter to
        -- undefined when its transaction ends, it returns it to the empty
        -- string. So on any connection a tenant transaction has already
        -- touched -- which, behind a pool, is every connection -- the value
        -- read here is '' and not NULL, and ''::uuid raises 22P02. nullif
        -- turns it back into NULL, and NULL matches no row.
        EXECUTE format($policy$
            CREATE POLICY %I ON public.%I
                USING (org_id = nullif(current_setting('app.current_org', true), '')::uuid)
                WITH CHECK (org_id = nullif(current_setting('app.current_org', true), '')::uuid)
        $policy$, t || '_tenant_isolation', t);
    END LOOP;
END
$$;

COMMIT;
