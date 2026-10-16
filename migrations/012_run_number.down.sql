-- Reverses 012_run_number.up.sql.
--
-- The numbers are dropped with the columns, and are not given back: a run
-- created after a rollback and a new migration would be numbered again from the
-- order of creation, which is not the numbers people were told in between.

BEGIN;

DROP TRIGGER IF EXISTS run_assign_number ON run;
DROP FUNCTION IF EXISTS run_assign_number();
DROP INDEX IF EXISTS run_org_project_number_idx;
ALTER TABLE run DROP CONSTRAINT IF EXISTS run_number_positive;
ALTER TABLE run DROP COLUMN number;
ALTER TABLE project DROP COLUMN run_counter;

COMMIT;
