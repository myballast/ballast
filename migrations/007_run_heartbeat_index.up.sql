BEGIN;

-- What the collector scans, every half minute, for ever.
--
-- It asks for the runs of one organization that are still being executed, or
-- were abandoned mid-execution. Those are a handful of rows in a table that
-- grows with every run ever launched, so the index is partial: it holds the
-- rows the question is about and nothing else, and a finished run leaves it the
-- moment it finishes.
--
-- heartbeat_at is the third column because the first two narrow the scan and
-- this one is the comparison.
CREATE INDEX run_org_unfinished_heartbeat_idx
    ON run (org_id, status, heartbeat_at)
    WHERE status IN ('running', 'orphaned');

COMMIT;
