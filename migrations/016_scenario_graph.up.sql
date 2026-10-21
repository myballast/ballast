BEGIN;

-- A scenario is written either as a script or as a graph. The graph is what the
-- canvas, the imports and the text editor all produce, and the script of a
-- graph scenario is generated from it.
--
-- The generated script is stored rather than rebuilt on the way out. A run
-- freezes the version it executed, and a correction to the generator must never
-- change what a past version produced -- the same reason scenario_version
-- exists at all.

ALTER TABLE scenario
    ADD COLUMN mode  text  NOT NULL DEFAULT 'code' CHECK (mode IN ('code', 'graph')),
    -- Null for a scenario written as code, which is every scenario that exists
    -- today.
    ADD COLUMN graph jsonb;

ALTER TABLE scenario_version
    ADD COLUMN mode  text  NOT NULL DEFAULT 'code' CHECK (mode IN ('code', 'graph')),
    ADD COLUMN graph jsonb;

-- A graph scenario without its graph would be a script nobody could edit and
-- nothing could regenerate; a code scenario with one would be two sources of
-- truth. Both are refused by the database rather than by discipline.
ALTER TABLE scenario
    ADD CONSTRAINT scenario_graph_matches_mode
    CHECK ((mode = 'graph') = (graph IS NOT NULL));

ALTER TABLE scenario_version
    ADD CONSTRAINT scenario_version_graph_matches_mode
    CHECK ((mode = 'graph') = (graph IS NOT NULL));

COMMIT;
