-- +goose Up
-- Foundation: extensions, RLS helper and grants. Tables come in later migrations.

CREATE EXTENSION IF NOT EXISTS citext;
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- Current tenant for RLS policies. Returns NULL when unset, so policies
-- comparing tenant_id = app_current_tenant() match nothing (fail closed).
-- +goose StatementBegin
CREATE FUNCTION app_current_tenant() RETURNS uuid
LANGUAGE sql STABLE AS $$
  SELECT NULLIF(current_setting('app.tenant_id', true), '')::uuid
$$;
-- +goose StatementEnd

-- Shared updated_at trigger function.
-- +goose StatementBegin
CREATE FUNCTION set_updated_at() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  NEW.updated_at := now();
  RETURN NEW;
END
$$;
-- +goose StatementEnd

-- The app role (created outside migrations, see deploy/postgres/init) gets
-- DML on every table this owner creates from now on. No DDL, no ownership.
ALTER DEFAULT PRIVILEGES IN SCHEMA public
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO claviq_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
  GRANT USAGE, SELECT ON SEQUENCES TO claviq_app;
GRANT EXECUTE ON FUNCTION app_current_tenant() TO claviq_app;

-- +goose Down
ALTER DEFAULT PRIVILEGES IN SCHEMA public
  REVOKE USAGE, SELECT ON SEQUENCES FROM claviq_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
  REVOKE SELECT, INSERT, UPDATE, DELETE ON TABLES FROM claviq_app;
DROP FUNCTION IF EXISTS set_updated_at();
DROP FUNCTION IF EXISTS app_current_tenant();
-- Extensions are left in place intentionally.
