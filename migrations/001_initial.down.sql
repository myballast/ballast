-- Reverses 001_initial.up.sql.
--
-- Policies and indexes belong to their table and disappear with it, so dropping
-- the tables is enough. The order below is the reverse of creation; CASCADE is
-- deliberately not used, so that a forgotten dependency fails loudly here
-- instead of silently dropping something this migration never created.

BEGIN;

DROP TABLE IF EXISTS audit_log;
DROP TABLE IF EXISTS run_log;
DROP TABLE IF EXISTS run_artifact;
DROP TABLE IF EXISTS run_threshold;
DROP TABLE IF EXISTS run_metric;
DROP TABLE IF EXISTS run;
DROP TABLE IF EXISTS threshold;
DROP TABLE IF EXISTS scenario;
DROP TABLE IF EXISTS secret;
DROP TABLE IF EXISTS environment;
DROP TABLE IF EXISTS membership;
DROP TABLE IF EXISTS project_allowed_host;
DROP TABLE IF EXISTS project;
DROP TABLE IF EXISTS app_user;
DROP TABLE IF EXISTS organization;

COMMIT;
