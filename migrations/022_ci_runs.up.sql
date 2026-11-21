-- A run launched by a pipeline has no person: an API token launched it. Both the
-- run and the audit entry about it have to say which token, and neither may
-- leave it blank — an entry with no author reads as Ballast acting alone, and a
-- credential somebody holds must not be mistaken for that.

BEGIN;

-- run.triggered_by_token
--
-- A new column, and not triggered_by with a different meaning: triggered_by is
-- cleared by its foreign key when a person leaves, so a run with a null there is
-- either a pipeline's or a former member's, and nothing in the row would say
-- which.
--
-- The composite key says the token is this organization's, which row-level
-- security cannot: it checks the row's own org_id and nothing about what the row
-- points at. ON DELETE SET NULL names the column, as for the person: clearing the
-- whole composite key would try to empty org_id, which is NOT NULL. A token goes
-- only with its project, which takes the project's runs along.
ALTER TABLE run ADD COLUMN triggered_by_token uuid;

ALTER TABLE run
    ADD CONSTRAINT run_org_id_triggered_by_token_fkey
    FOREIGN KEY (org_id, triggered_by_token) REFERENCES api_token (org_id, id)
    ON DELETE SET NULL (triggered_by_token);

-- A person or a token, never both. A run that named both is a run nobody could
-- say who launched.
ALTER TABLE run
    ADD CONSTRAINT run_triggered_by_one_kind
    CHECK (triggered_by IS NULL OR triggered_by_token IS NULL);

-- The limit on a token's runs is asked on every request a pipeline makes: how
-- many in the last hour, and how long until the oldest leaves the window. Only
-- the rows that have a token, which are few next to the rest.
CREATE INDEX run_org_token_created_idx
    ON run (org_id, triggered_by_token, created_at DESC)
    WHERE triggered_by_token IS NOT NULL;

-- audit_log.actor_token_id
--
-- Same reason, same shape, with one difference: no foreign key. An entry outlives
-- the token it names and keeps naming it, as it outlives its project, which has
-- no key either (009). A key that cleared the author when the token went — and a
-- token goes when its project is deleted — would make deleting a project a way of
-- erasing what its credentials did.
ALTER TABLE audit_log ADD COLUMN actor_token_id uuid;

ALTER TABLE audit_log
    ADD CONSTRAINT audit_log_actor_one_kind
    CHECK (actor_id IS NULL OR actor_token_id IS NULL);

COMMIT;
