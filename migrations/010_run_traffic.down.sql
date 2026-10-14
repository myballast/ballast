BEGIN;

DROP INDEX IF EXISTS run_org_ended_idx;

ALTER TABLE run DROP COLUMN IF EXISTS traffic_measured;
ALTER TABLE run DROP COLUMN IF EXISTS data_received_bytes;
ALTER TABLE run DROP COLUMN IF EXISTS data_sent_bytes;
ALTER TABLE run DROP COLUMN IF EXISTS ingress_bytes;

COMMIT;
