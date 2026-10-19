-- Reverses 015_project_details_and_favorites.up.sql. The details people wrote and
-- the stars they set are dropped with their columns and table.

BEGIN;

DROP TABLE IF EXISTS project_favorite;

ALTER TABLE project
    DROP CONSTRAINT IF EXISTS project_stack_size,
    DROP CONSTRAINT IF EXISTS project_owner_length,
    DROP CONSTRAINT IF EXISTS project_region_length,
    DROP CONSTRAINT IF EXISTS project_infra_length,
    DROP COLUMN IF EXISTS stack,
    DROP COLUMN IF EXISTS owner,
    DROP COLUMN IF EXISTS region,
    DROP COLUMN IF EXISTS infra;

COMMIT;
