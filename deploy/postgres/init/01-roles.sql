-- Runs once when the local Postgres volume is first created.
-- In production, run the equivalent manually with a strong password.

-- Runtime role for the Go service: not the table owner, cannot bypass RLS.
CREATE ROLE claviq_app LOGIN PASSWORD 'claviq_app' NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS;
GRANT CONNECT ON DATABASE claviq_desk TO claviq_app;
GRANT USAGE ON SCHEMA public TO claviq_app;
