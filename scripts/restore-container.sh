#!/bin/sh
set -eu
test -f /restore/COMPLETE || { echo 'Incomplete backup; refusing restore.' >&2; exit 1; }
(cd /restore && sha256sum -c SHA256SUMS)
test -z "$(find /data -mindepth 1 -maxdepth 1 -print -quit)" || { echo 'Application data directory is not empty; use a fresh restore project.' >&2; exit 1; }
PGPASSWORD="$(tr -d '\r\n' < /run/secrets/app_database_password)"
export PGPASSWORD
tables="$(psql -X -A -t -c "SELECT count(*) FROM information_schema.tables WHERE table_schema='public'")"
test "$tables" = 0 || { echo 'Database contains tables; use a fresh restore project.' >&2; exit 1; }
# Only accept our regular-file/directory archive format, never links or traversal.
tar -tzf /restore/files.tar.gz | awk '/^\// || /(^|\/)\.\.(\/|$)/ { bad=1 } END { exit bad }'
tar -tvzf /restore/files.tar.gz | awk 'substr($0,1,1) != "d" && substr($0,1,1) != "-" { bad=1 } END { exit bad }'
tar -xzf /restore/files.tar.gz -C /data
pg_restore --exit-on-error --single-transaction --no-owner --no-acl --dbname="$PGDATABASE" /restore/database.dump
unset PGPASSWORD
printf '%s\n' 'Restore complete. Validate users, open timers, rates, invoices and PDF downloads before opening traffic.'
