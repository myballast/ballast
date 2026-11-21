-- Reverses 023_run_ci.up.sql.

BEGIN;

ALTER TABLE run
    DROP CONSTRAINT IF EXISTS run_ci_job_url_check,
    DROP CONSTRAINT IF EXISTS run_ci_commit_check,
    DROP CONSTRAINT IF EXISTS run_ci_branch_check,
    DROP CONSTRAINT IF EXISTS run_ci_repository_check,
    DROP CONSTRAINT IF EXISTS run_ci_needs_provider,
    DROP CONSTRAINT IF EXISTS run_ci_provider_check;

ALTER TABLE run
    DROP COLUMN IF EXISTS ci_job_url,
    DROP COLUMN IF EXISTS ci_commit,
    DROP COLUMN IF EXISTS ci_branch,
    DROP COLUMN IF EXISTS ci_repository,
    DROP COLUMN IF EXISTS ci_provider;

COMMIT;
