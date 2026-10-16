#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
# Quiesce writes for a matched database/file snapshot. Work clocks stay running.
running="$(docker compose ps --status running --services app)"
resume() { if [ "$running" = app ]; then docker compose start app >/dev/null; fi; }
trap resume EXIT HUP INT TERM
docker compose stop app
docker compose --profile backup run --rm backup
printf '%s\n' 'Copy the COMPLETE backup and encrypted .secrets/.env off-host. Local volumes are not backups.'
