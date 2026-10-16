#!/bin/sh
set -eu
APP_DB_PASSWORD="$(tr -d '\r\n' < /run/secrets/app_database_password)"
export APP_DB_PASSWORD
psql -v ON_ERROR_STOP=1 --username postgres --dbname seliworks <<'SQL'
\getenv app_password APP_DB_PASSWORD
CREATE ROLE seliworks WITH LOGIN PASSWORD :'app_password' NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION;
ALTER DATABASE seliworks OWNER TO seliworks;
ALTER SCHEMA public OWNER TO seliworks;
SQL
unset APP_DB_PASSWORD
