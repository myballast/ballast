-- A project's webhook: the way a tool that is neither GitHub nor GitLab starts a run, by
-- signing a request with a secret the project holds.
--
-- The secret is stored sealed (AES-GCM, the same cipher as an environment's secrets) and
-- not hashed: verifying a signature means computing the HMAC again, which needs the
-- secret itself. It is shown once, when it is made or replaced, and never returned.

BEGIN;

CREATE TABLE project_webhook (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id           uuid NOT NULL REFERENCES organization (id) ON DELETE CASCADE,
    project_id       uuid NOT NULL,
    -- Sealed, and never empty: an empty value would be a project that verifies
    -- anything signed with an empty key.
    secret_encrypted bytea NOT NULL CHECK (length(secret_encrypted) > 0),
    -- Cleared, not cascaded, when that person leaves: a pipeline must not stop because
    -- somebody did.
    created_by       uuid,
    created_at       timestamptz NOT NULL DEFAULT now(),
    rotated_at       timestamptz,

    UNIQUE (org_id, id),
    -- One webhook per project. Replacing the secret is a rotation of this row, not a
    -- second one: the address a tool calls does not change.
    UNIQUE (org_id, project_id),
    FOREIGN KEY (org_id, project_id) REFERENCES project (org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, created_by) REFERENCES app_user (org_id, id) ON DELETE SET NULL (created_by)
);

ALTER TABLE project_webhook ENABLE ROW LEVEL SECURITY;
ALTER TABLE project_webhook FORCE ROW LEVEL SECURITY;

CREATE POLICY project_webhook_tenant_isolation ON project_webhook
    USING (org_id = nullif(current_setting('app.current_org', true), '')::uuid)
    WITH CHECK (org_id = nullif(current_setting('app.current_org', true), '')::uuid);

-- The deliveries already served, so that a request is not served twice. The primary key
-- is what decides it: two requests with the same identifier racing each other do not
-- both find it absent and both insert, one of them loses on the key.
CREATE TABLE webhook_delivery (
    org_id      uuid NOT NULL REFERENCES organization (id) ON DELETE CASCADE,
    project_id  uuid NOT NULL,
    -- The shape of the header: 16 to 64 of letters, digits, '-' and '_'. The pattern is
    -- anchored with \A and \Z: a '$' would let a trailing newline through.
    delivery_id text NOT NULL CHECK (
        length(delivery_id) BETWEEN 16 AND 64 AND delivery_id ~ '\A[A-Za-z0-9_-]+\Z'
    ),
    received_at timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (org_id, project_id, delivery_id),
    FOREIGN KEY (org_id, project_id) REFERENCES project (org_id, id) ON DELETE CASCADE
);

-- Forgetting what is past the retention reads by date, within an organization.
CREATE INDEX webhook_delivery_org_received_idx ON webhook_delivery (org_id, received_at);

ALTER TABLE webhook_delivery ENABLE ROW LEVEL SECURITY;
ALTER TABLE webhook_delivery FORCE ROW LEVEL SECURITY;

CREATE POLICY webhook_delivery_tenant_isolation ON webhook_delivery
    USING (org_id = nullif(current_setting('app.current_org', true), '')::uuid)
    WITH CHECK (org_id = nullif(current_setting('app.current_org', true), '')::uuid);

-- run.triggered_by_webhook
--
-- A column of its own, as for a token: triggered_by is cleared when a person leaves, so
-- nothing else in the row would say a webhook did it.
--
-- No foreign key, on purpose and unlike triggered_by_token. A token goes only with its
-- project, which takes the project's runs along. A webhook is deleted by an
-- administrator while its project and its runs stay, and a key that cleared the column
-- then would turn "launched by the webhook" into a run with no author, which the journal
-- reads as Ballast acting alone. The id is written by the service from the webhook it
-- just resolved under the organization, never taken from a request.
ALTER TABLE run ADD COLUMN triggered_by_webhook uuid;

-- A person, a token or a webhook: at most one. A run that named two is a run nobody
-- could say who launched.
ALTER TABLE run DROP CONSTRAINT run_triggered_by_one_kind;
ALTER TABLE run
    ADD CONSTRAINT run_triggered_by_one_kind
    CHECK (num_nonnulls(triggered_by, triggered_by_token, triggered_by_webhook) <= 1);

-- The limit on a webhook's runs is asked on every request: how many in the last hour,
-- and how long until the oldest leaves the window.
CREATE INDEX run_org_webhook_created_idx
    ON run (org_id, triggered_by_webhook, created_at DESC)
    WHERE triggered_by_webhook IS NOT NULL;

-- audit_log.actor_webhook_id: same reason, same shape, and no foreign key, as for
-- actor_token_id: an entry outlives what it names.
ALTER TABLE audit_log ADD COLUMN actor_webhook_id uuid;

ALTER TABLE audit_log DROP CONSTRAINT audit_log_actor_one_kind;
ALTER TABLE audit_log
    ADD CONSTRAINT audit_log_actor_one_kind
    CHECK (num_nonnulls(actor_id, actor_token_id, actor_webhook_id) <= 1);

COMMIT;
