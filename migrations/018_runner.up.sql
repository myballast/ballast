-- The presence of the machines that execute runs.
--
-- A worker writes one row about itself and keeps it fresh: it is the only thing
-- that knows it is alive. Nothing is registered by anybody else and nothing is
-- issued to it -- its credential is the database role it already connects with
-- (docs/adr/0001-runner-connection.md), and a revocable runner token belongs to
-- the design that ADR sets aside.
--
-- "Online" is not stored. A row says what the worker last said and when; whether
-- that is recent enough is decided when somebody reads it, so that a worker that
-- was killed without a word is offline without anything having to write that.
-- state is what the worker declares, and stopped is the one a clean exit writes.
--
-- contract is the version of what the worker and the database promise each
-- other: a column the worker writes, a status it knows. It is an integer because
-- a minimum has to be compared, and a commit hash cannot be. version is the
-- build, for a person reading the screen.
--
-- runner_id on environment stays reserved and unused: this is Ballast's own
-- runner, not the one phase 3 puts at a customer's.

BEGIN;

CREATE TABLE runner (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id       uuid NOT NULL REFERENCES organization (id) ON DELETE CASCADE,
    name         text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 80),
    version      text NOT NULL CHECK (char_length(version) BETWEEN 1 AND 64),
    contract     integer NOT NULL CHECK (contract >= 1),
    state        text NOT NULL CHECK (state IN ('serving', 'draining', 'stopped')),
    started_at   timestamptz NOT NULL,
    heartbeat_at timestamptz NOT NULL,

    -- A runner restarted under the same name is the same runner: it takes its
    -- row back instead of leaving a dead one beside it.
    UNIQUE (org_id, name)
);

ALTER TABLE runner ENABLE ROW LEVEL SECURITY;
ALTER TABLE runner FORCE ROW LEVEL SECURITY;

CREATE POLICY runner_tenant_isolation ON runner
    USING (org_id = nullif(current_setting('app.current_org', true), '')::uuid)
    WITH CHECK (org_id = nullif(current_setting('app.current_org', true), '')::uuid);

COMMIT;
