-- Reverses 002_run_metric_stat.up.sql.
--
-- Restoring the narrower constraint fails if more than one statistic was stored
-- for the same metric and tag, which is the normal case once any run has been
-- recorded. That is the honest behaviour: the data no longer fits the old
-- shape, and dropping rows to make it fit is not something a migration should
-- decide on its own.

BEGIN;

ALTER TABLE run_metric DROP CONSTRAINT run_metric_org_id_run_id_name_tag_name_stat_key;
ALTER TABLE run_metric ADD CONSTRAINT run_metric_org_id_run_id_name_tag_name_key
    UNIQUE (org_id, run_id, name, tag_name);

ALTER TABLE run_metric DROP COLUMN stat;

COMMIT;
