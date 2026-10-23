#!/bin/sh
# Runs once, on first initialization of the data volume, as the superuser.
# The role itself is defined in ops/sql/00-app-role.sql, the same file production
# uses, so that CI exercises the script that will actually run there.
set -eu

psql -v ON_ERROR_STOP=1 --no-psqlrc \
    --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" \
    -v app_password="${BALLAST_APP_PASSWORD:-ballast_app}" \
    -f /ops/sql/00-app-role.sql
