#!/usr/bin/env bash
# Daily backup, stored OFF the VPS:
#   1. pg_dump (custom format) -> encrypted with age -> uploaded to the backup bucket
#   2. R2 storage bucket (PDFs, logos, fonts) mirrored to the backup bucket
# Requires: pg_dump, age, rclone (remotes "r2" = live bucket, "backup" = off-site target).
# Env (backup.env): BACKUP_DATABASE_URL, AGE_RECIPIENT, R2_BUCKET, BACKUP_BUCKET, KEEP_DAYS
set -euo pipefail

stamp=$(date -u +%Y%m%dT%H%M%SZ)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

pg_dump --format=custom --no-owner --dbname="$BACKUP_DATABASE_URL" \
  | age --recipient "$AGE_RECIPIENT" > "$tmp/db-$stamp.dump.age"
rclone copyto "$tmp/db-$stamp.dump.age" "backup:$BACKUP_BUCKET/postgres/db-$stamp.dump.age"

rclone sync "r2:$R2_BUCKET" "backup:$BACKUP_BUCKET/storage" --fast-list

# Retention for database dumps.
rclone delete "backup:$BACKUP_BUCKET/postgres" --min-age "${KEEP_DAYS:-30}d"
echo "backup $stamp ok"
