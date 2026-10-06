-- Reverses 004_scenario_version.up.sql.
--
-- Dropping the table takes the history of every script with it: scenario only
-- ever holds the current one. That is what rolling this back means, and it is
-- the reason the down migration says so here rather than looking harmless.

BEGIN;

ALTER TABLE run DROP CONSTRAINT run_scenario_version_fkey;
DROP TABLE scenario_version;

COMMIT;
