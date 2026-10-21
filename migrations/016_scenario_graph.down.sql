BEGIN;

ALTER TABLE scenario_version
    DROP CONSTRAINT scenario_version_graph_matches_mode,
    DROP COLUMN graph,
    DROP COLUMN mode;

ALTER TABLE scenario
    DROP CONSTRAINT scenario_graph_matches_mode,
    DROP COLUMN graph,
    DROP COLUMN mode;

COMMIT;
