-- Reverses 018_runner.up.sql. The presence rows are dropped with their table; they
-- are rewritten by the next heartbeat of every worker, so nothing is lost that a
-- worker does not say again within ten seconds.

BEGIN;

DROP TABLE IF EXISTS runner;

COMMIT;
