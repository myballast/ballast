-- A k6 trend metric reports six numbers for the same metric and tag: the
-- average, the minimum, the median, the maximum and two percentiles. Migration
-- 001 gave run_metric a single value per (name, tag_name), which can hold one
-- of the six and loses the rest.
--
-- The alternative was folding the statistic into the name, as
-- http_req_duration.p(95). A column keeps the two apart, which is what the run
-- detail screen needs to ask for one percentile without parsing a string.

BEGIN;

ALTER TABLE run_metric ADD COLUMN stat text NOT NULL DEFAULT '';

ALTER TABLE run_metric DROP CONSTRAINT run_metric_org_id_run_id_name_tag_name_key;
ALTER TABLE run_metric ADD CONSTRAINT run_metric_org_id_run_id_name_tag_name_stat_key
    UNIQUE (org_id, run_id, name, tag_name, stat);

COMMIT;
