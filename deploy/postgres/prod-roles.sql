-- Run once on the production database as a superuser, before `server migrate up`.
-- Replace the passwords. The backend's MIGRATE_DATABASE_URL uses claviq (owner);
-- DATABASE_URL uses claviq_app (no ownership, cannot bypass RLS).
CREATE ROLE claviq LOGIN PASSWORD 'CHANGE-ME-owner';
CREATE ROLE claviq_app LOGIN PASSWORD 'CHANGE-ME-app' NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS;
CREATE DATABASE claviq_desk OWNER claviq;
\c claviq_desk
GRANT CONNECT ON DATABASE claviq_desk TO claviq_app;
GRANT USAGE ON SCHEMA public TO claviq_app;
-- Read-only role for backups.
CREATE ROLE claviq_backup LOGIN PASSWORD 'CHANGE-ME-backup' BYPASSRLS;
GRANT pg_read_all_data TO claviq_backup;
