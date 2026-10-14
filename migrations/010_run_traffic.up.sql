BEGIN;

-- What a run moved, measured twice.
--
-- egress_bytes has been here since migration 001, reserved and never written.
-- It takes the figure that matters: what left the machine for the target, as
-- counted by the egress proxy, which is the only component that sees it. The
-- generator never does — it counts what its script sent, which is that minus
-- TLS records and minus every header.
--
-- Both measurements are kept, and not because one is a backup for the other.
-- Their difference is the overhead this installation pays, which is exactly
-- what has to be known before a price can be put on a gigabyte.
ALTER TABLE run ADD COLUMN ingress_bytes bigint NOT NULL DEFAULT 0
    CHECK (ingress_bytes >= 0);

-- The generator's own figures, from its summary. They are also among the run's
-- metrics, as data_sent and data_received; they are copied here for one reason
-- — summing a month must not mean reading every metric of every run — and the
-- two copies are written from the same parsed summary in the same transaction,
-- so they cannot drift apart.
ALTER TABLE run ADD COLUMN data_sent_bytes bigint NOT NULL DEFAULT 0
    CHECK (data_sent_bytes >= 0);
ALTER TABLE run ADD COLUMN data_received_bytes bigint NOT NULL DEFAULT 0
    CHECK (data_received_bytes >= 0);

-- Whether anybody counted.
--
-- Zero means a run that moved nothing; this tells that apart from a run whose
-- proxy could not be read or whose summary carried an impossible counter. A
-- month's total that cannot say how much of it is unmeasured is a month's total
-- nobody can put a price on.
ALTER TABLE run ADD COLUMN traffic_measured boolean NOT NULL DEFAULT false;

-- The monthly reading: one organization, the runs that ended inside a window.
-- Partial, because a run with no end has no volumes either.
CREATE INDEX run_org_ended_idx ON run (org_id, ended_at) WHERE ended_at IS NOT NULL;

COMMIT;
