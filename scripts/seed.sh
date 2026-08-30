#!/usr/bin/env bash
# Loads sample data into the local environment.
#
# There is no domain data yet: the seeds arrive with each domain's PRD. What this does today
# is create the MinIO bucket the media needs, which is the only piece of local state that
# does not come from a migration.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
COMPOSE="docker compose -f $ROOT/deploy/docker-compose.yml"

echo "==> creating the MinIO bucket"

$COMPOSE exec -T minio sh -c '
  mc alias set local http://localhost:9000 dizen dizen12345 >/dev/null 2>&1
  mc mb --ignore-existing local/dizen-media
  mc anonymous set none local/dizen-media
' || {
  echo "error: MinIO did not answer; is the environment up?" >&2
  exit 1
}

for svc in identity tours booking admin mail-dispatcher; do
  seed="$ROOT/deploy/seed/$svc.sql"

  if [ -f "$seed" ]; then
    echo "==> seeding $svc"
    $COMPOSE exec -T "$svc-db" psql -U dizen -d "$(echo "$svc" | tr - _)_db" < "$seed"
  fi
done

echo "==> done"
