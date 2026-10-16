#!/bin/sh
set -eu
if [ "$#" -ne 1 ]; then echo "Usage: $0 /absolute/path/to/COMPLETE-backup-directory" >&2; exit 1; fi
RESTORE_DIR="$(cd "$1" && pwd -P)"
export RESTORE_DIR
cd "$(dirname "$0")/.."
test -n "${COMPOSE_PROJECT_NAME:-}" || { echo 'Set a new COMPOSE_PROJECT_NAME (for example seliworks-restore); never restore over a live deployment.' >&2; exit 1; }
test "$COMPOSE_PROJECT_NAME" != seliworks || { echo 'Use a separate restore project.' >&2; exit 1; }
docker compose stop app
docker compose up -d postgres
# Populate the empty named volume from the runtime image's UID10001-owned directory.
docker compose run --rm --no-deps --entrypoint /bin/true app
docker compose --profile backup run --rm restore
