# Deploying Claviq Desk (single VPS)

Layout on the server:

```
/opt/claviq-desk/backend/server     Go binary (API + MCP + OAuth)
/opt/claviq-desk/web/               Next.js standalone build (server.js, .next/static, public)
/opt/claviq-desk/deploy/            this folder
/etc/claviq-desk/backend.env        backend environment (see backend/.env.example)
/var/lib/claviq-desk/               DATA_DIR: font cache (+ storage if STORAGE_DRIVER=local)
```

## One-time setup

1. Packages: PostgreSQL 16+, Caddy, Node 22+, `typst` (single binary, put on PATH), `rclone`, `age`.
2. Database: run `deploy/postgres/prod-roles.sql` as a superuser (change the passwords).
3. User and dirs: `useradd --system claviq`; create `/opt/claviq-desk`, `/var/lib/claviq-desk` (owned by `claviq`).
4. `/etc/claviq-desk/backend.env` (mode 600), based on `backend/.env.example`:
   - `APP_ENV=production`, `HTTP_ADDR=127.0.0.1:8080`, `PUBLIC_URL=https://desk.claviq.com`
   - `APP_SECRET=$(openssl rand -hex 32)` — rotating it signs everyone out and invalidates links/tokens
   - `DATABASE_URL` (claviq_app), `MIGRATE_DATABASE_URL` (claviq)
   - `DATA_DIR=/var/lib/claviq-desk`, `STORAGE_DRIVER=r2` + `R2_*` credentials
5. Caddy: copy `deploy/Caddyfile` to `/etc/caddy/Caddyfile`, `systemctl reload caddy`.
6. systemd: copy `deploy/systemd/*` to `/etc/systemd/system/`, then
   `systemctl enable --now claviq-backend claviq-web claviq-backup.timer`.
7. Create the first business: `sudo -u claviq env $(cat /etc/claviq-desk/backend.env) /opt/claviq-desk/backend/server create-tenant`.

## Releasing

```
# backend (cross-compile from a dev machine)
cd backend && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o server ./cmd/server
# web
cd web && npm ci && npm run build
#   upload: .next/standalone/* -> /opt/claviq-desk/web/, .next/static -> web/.next/static, public -> web/public
systemctl restart claviq-backend claviq-web   # migrations run on backend start
```

## Backups

`claviq-backup.timer` runs `deploy/backup/backup.sh` daily at 02:30 Colombo time:
an age-encrypted `pg_dump` and an `rclone sync` of the R2 bucket, both to a separate
backup bucket/provider (`backup:` rclone remote). Configure `/etc/claviq-desk/backup.env`:
`BACKUP_DATABASE_URL` (claviq_backup role), `AGE_RECIPIENT`, `R2_BUCKET`, `BACKUP_BUCKET`, `KEEP_DAYS`.
Keep the age private key off the VPS. Test a restore with `age -d … | pg_restore` periodically.

## Connecting AI clients

In Claude (Settings → Connectors → Add custom connector) or ChatGPT (connectors / developer mode),
add `https://desk.claviq.com/mcp`. The client discovers OAuth via
`/.well-known/oauth-protected-resource/mcp`, registers itself, and sends the owner to sign in and
approve. Connections can be revoked in Settings → AI & security.
