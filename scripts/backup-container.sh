#!/bin/sh
set -eu
umask 077
PGPASSWORD="$(tr -d '\r\n' < /run/secrets/app_database_password)"
export PGPASSWORD
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
target="/backups/$stamp"
mkdir "$target"
trap 'rm -f "$target/COMPLETE"' HUP INT TERM
pg_dump --format=custom --no-owner --no-acl --lock-wait-timeout=30s --file="$target/database.dump"
tar -czf "$target/files.tar.gz" -C /data .
printf '%s\n' "$stamp" > "$target/created-utc.txt"
(cd "$target" && sha256sum database.dump files.tar.gz > SHA256SUMS)
touch "$target/COMPLETE"
unset PGPASSWORD
printf 'Backup complete: %s\n' "$stamp"
