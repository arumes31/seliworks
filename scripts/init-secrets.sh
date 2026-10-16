#!/bin/sh
set -eu
umask 077
cd "$(dirname "$0")/.."
command -v openssl >/dev/null || { echo 'OpenSSL is required.' >&2; exit 1; }
mkdir -p .secrets backups
for item in postgres_password app_database_password bootstrap_password totp_key; do
  if [ ! -e ".secrets/$item" ]; then printf '%s\n' "$(openssl rand -hex 32 | tr -d '\r\n')" > ".secrets/$item"; fi
done
if [ ! -e .secrets/smtp_password ]; then : > .secrets/smtp_password; fi
if [ ! -e .secrets/database_url ]; then
  printf 'postgres://seliworks:%s@postgres:5432/seliworks?sslmode=disable\n' "$(tr -d '\r\n' < .secrets/app_database_password)" > .secrets/database_url
fi
# Compose bind-mounted secret files must be readable by the container UID.
# The parent directory remains mode 700, preventing other host users reading them.
chmod 700 .secrets
chmod 644 .secrets/*
printf '%s\n' 'Secrets ready. Read .secrets/bootstrap_password privately; never commit or paste it into logs.'
printf '%s\n' 'On Linux, grant UID 10001 ownership of backups: sudo chown 10001:10001 backups && chmod 700 backups'
