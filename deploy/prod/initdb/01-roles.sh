#!/bin/sh
# Runs once, when the Postgres volume is first created.
# claviq (POSTGRES_USER) owns the schema; claviq_app is the runtime role and can
# never bypass row-level security.
set -eu
psql -v ON_ERROR_STOP=1 -v app_pw="$APP_DB_PASSWORD" \
     --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<'SQL'
CREATE ROLE claviq_app LOGIN PASSWORD :'app_pw' NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS;
GRANT CONNECT ON DATABASE claviq_desk TO claviq_app;
GRANT USAGE ON SCHEMA public TO claviq_app;
SQL
