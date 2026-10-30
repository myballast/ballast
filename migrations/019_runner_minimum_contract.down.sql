-- Reverses 019_runner_minimum_contract.up.sql. The minimum is dropped with its
-- column; every worker is admitted again.

BEGIN;

ALTER TABLE organization DROP COLUMN IF EXISTS min_runner_contract;

COMMIT;
