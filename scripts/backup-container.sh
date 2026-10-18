#!/bin/sh
set -eu
umask 077
PGPASSWORD="$(tr -d '\r\n' < /run/secrets/app_database_password)"
export PGPASSWORD
stamp="${BACKUP_ID:-$(date -u +%Y%m%dT%H%M%SZ)}"
case "$stamp" in
  [0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]T[0-9][0-9][0-9][0-9][0-9][0-9]Z) ;;
  *) echo 'Invalid backup ID.' >&2; exit 1 ;;
esac
target="/backups/$stamp"
mkdir "$target"
trap 'rm -f "$target/COMPLETE"' HUP INT TERM
pg_dump --format=custom --no-owner --no-acl --lock-wait-timeout=30s --file="$target/database.dump"
tar -czf "$target/files.tar.gz" -C /data .
printf '%s\n' "$stamp" > "$target/created-utc.txt"
(cd "$target" && sha256sum database.dump files.tar.gz > SHA256SUMS)
touch "$target/COMPLETE"
unset PGPASSWORD
printf 'Database and file snapshot created: %s\n' "$stamp"
