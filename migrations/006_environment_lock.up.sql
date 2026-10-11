BEGIN;

-- One run at a time per environment.
--
-- Two runs against the same target at once produce two sets of measurements,
-- both wrong: each is measuring the other's load as well as its own. Nothing in
-- the numbers says so, which is what makes it worth a table.
--
-- The lock is a row rather than a PostgreSQL advisory lock, and the reason is
-- the duration. A transaction-scoped advisory lock is released at COMMIT, and
-- the claim commits before the run starts; a session-scoped one is held by the
-- connection, so behind a pool it would mean immobilising a connection for the
-- length of a soak test. Neither has an expiry, and a worker that dies holding
-- one of them would block its target until somebody noticed by hand.
CREATE TABLE environment_lock (
    org_id         uuid NOT NULL REFERENCES organization (id) ON DELETE CASCADE,
    environment_id uuid NOT NULL,
    -- Which run holds it, so that a release can say so and refuse to drop
    -- somebody else's.
    run_id      uuid NOT NULL,
    acquired_at timestamptz NOT NULL,
    -- When it stops counting.
    --
    -- This matters only when a worker dies: a live one releases the lock when
    -- the run ends. It is set from the run's own duration, already clamped to
    -- the environment's ceiling, plus a margin for starting a container and
    -- reading a summary — so a soak test and a smoke test do not block their
    -- target for the same length of time after a crash.
    expires_at timestamptz NOT NULL,

    -- The whole mechanism. Two workers that both pass the "is it free" filter
    -- meet here, and exactly one of them wins: the other's conditional upsert
    -- matches no row and it leaves its run queued.
    PRIMARY KEY (org_id, environment_id),

    FOREIGN KEY (org_id, environment_id) REFERENCES environment (org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, run_id) REFERENCES run (org_id, id) ON DELETE CASCADE,

    CHECK (expires_at > acquired_at)
);

-- Migration 001 walks the catalogue to apply row-level security, which covers
-- the tables that existed then. A table added later writes its own.
ALTER TABLE environment_lock ENABLE ROW LEVEL SECURITY;
ALTER TABLE environment_lock FORCE ROW LEVEL SECURITY;

CREATE POLICY environment_lock_tenant_isolation ON environment_lock
    USING (org_id = nullif(current_setting('app.current_org', true), '')::uuid)
    WITH CHECK (org_id = nullif(current_setting('app.current_org', true), '')::uuid);

COMMIT;
