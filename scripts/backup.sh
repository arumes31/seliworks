#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
umask 077
python3 scripts/installation-backup.py preflight .
# Read only the resolved mount path; never print the expanded Compose environment.
backup_root="$(docker compose --profile backup config --format json | python3 -c '
import json, sys
from pathlib import Path
mounts = json.load(sys.stdin)["services"]["backup"]["volumes"]
paths = [v["source"] for v in mounts if v["target"] == "/backups" and v["type"] == "bind"]
if len(paths) != 1 or not Path(paths[0]).is_absolute():
    sys.exit("Expected one absolute backup bind mount")
print(paths[0])
')"
test -d "$backup_root"
# Keep overlapping manual/timer backups from restarting each other's app.
exec 9>"$backup_root/.backup.lock"
flock -n 9 || { echo 'Another backup is running.' >&2; exit 1; }
BACKUP_ID="$(date -u +%Y%m%dT%H%M%SZ)"
export BACKUP_ID
# Quiesce writes for a matched database/file snapshot. Work clocks stay running.
running="$(docker compose ps --status running --services app)"
restart_app() {
  if [ "$running" = app ]; then
    running=''
    docker compose up -d --no-deps --no-recreate --wait --wait-timeout 120 app
  fi
}
resume() {
  status=$?
  trap - EXIT HUP INT TERM
  restart_app || status=1
  exit "$status"
}
trap resume EXIT
trap 'exit 1' HUP INT TERM
docker compose stop app
docker compose --profile backup run --rm -e BACKUP_ID="$BACKUP_ID" backup
python3 scripts/installation-backup.py capture . "$backup_root/$BACKUP_ID"
python3 scripts/installation-backup.py verify "$backup_root/$BACKUP_ID"
restart_app
trap - EXIT HUP INT TERM
printf 'Full installation backup complete: %s\n' "$BACKUP_ID"
printf '%s\n' 'Local protection uses Unix permissions, not encryption. Encrypt the entire backup before copying off-host.'
