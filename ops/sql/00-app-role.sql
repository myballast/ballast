-- Creates (or re-asserts) the role the API and the worker connect as.
--
-- Ballast isolates organizations with Row-Level Security. A superuser, a role
-- with BYPASSRLS and the owner of a table all read straight through it, and the
-- default user of a managed PostgreSQL such as Railway's is a superuser. Without
-- this role there is no isolation between customers in production, whatever the
-- policies say.
--
-- Run it ONCE BEFORE THE FIRST MIGRATION, as the owner role (the one that will
-- run the migrations and own the tables), and again whenever the password
-- changes. The password is a psql variable, never part of this file:
--
--   psql "$MIGRATE_ADMIN_URL" -v ON_ERROR_STOP=1 \
--        -v app_password="$BALLAST_APP_PASSWORD" -f ops/sql/00-app-role.sql
--
-- Order matters. Default privileges only reach tables created afterwards, and
-- that is what keeps audit_log append-only: migration 009 takes UPDATE and
-- DELETE away from the role, and a blanket GRANT ... ON ALL TABLES run later
-- would hand them back. This script therefore never grants on existing tables.
--
-- Safe to run twice. Needs psql 12 or later.

-- \quit would exit with status 0 and let a deployment carry on without the role.
-- An error under ON_ERROR_STOP is what makes psql exit non-zero.
\if :{?app_password}
\else
    DO $$ BEGIN
        RAISE EXCEPTION 'app_password is not set: pass it with  -v app_password=...';
    END $$;
\endif

-- The password goes through format(%L), so it is quoted as a literal whatever it
-- contains. Server logs record statements when log_statement is 'all' or 'ddl':
-- use a generated password, and rotate it if that setting was on.
SELECT format(
    'CREATE ROLE ballast_app LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS NOREPLICATION',
    :'app_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'ballast_app')
\gexec

-- Also runs on a role that already existed: re-asserts every attribute, so a
-- role someone loosened by hand is tightened again.
SELECT format(
    'ALTER ROLE ballast_app WITH LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS NOREPLICATION',
    :'app_password')
\gexec

-- The database name is not ours to choose: Railway calls it "railway".
SELECT format('GRANT CONNECT ON DATABASE %I TO ballast_app', current_database())
\gexec

-- TEMPORARY is granted to PUBLIC by default. A temporary table lives in
-- pg_temp, which is searched before public: a role that can create one can
-- shadow a real table for its own session. Ballast uses none, so nobody gets it.
SELECT format('REVOKE TEMPORARY ON DATABASE %I FROM PUBLIC', current_database())
\gexec

GRANT USAGE ON SCHEMA public TO ballast_app;

-- Tables and sequences created later by the role running this script, so by the
-- migrations, are reachable by the application role, still filtered by the RLS
-- policies that come with them. No CREATE, no DDL, no TRUNCATE, no REFERENCES.
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO ballast_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT USAGE, SELECT ON SEQUENCES TO ballast_app;

-- golang-migrate keeps its own bookkeeping table. It carries no org_id and no
-- policy, and it has no business sitting among the application tables: the
-- schema tests assert that every table in public is tenant-scoped, with no
-- exception list to hide behind. So the migration tool gets its own schema,
-- which the application role cannot see at all.
CREATE SCHEMA IF NOT EXISTS migrate;
REVOKE ALL ON SCHEMA migrate FROM PUBLIC;
