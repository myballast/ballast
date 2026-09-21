-- Runs once, as the superuser, on first initialization of the data volume.
--
-- Ballast relies on Row-Level Security for tenant isolation. A superuser and a
-- table owner both bypass RLS, so the application must never connect with the
-- role that owns the schema. This creates the least-privileged role the API and
-- the worker use; migrations keep running as the owner (ballast).

CREATE ROLE ballast_app LOGIN PASSWORD 'ballast_app'
    NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS;

GRANT CONNECT ON DATABASE ballast TO ballast_app;
GRANT USAGE ON SCHEMA public TO ballast_app;

-- Tables created later by the owner are reachable by the application role,
-- still filtered by the RLS policies that come with migration 001.
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO ballast_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT USAGE, SELECT ON SEQUENCES TO ballast_app;
