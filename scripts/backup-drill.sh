#!/usr/bin/env bash
# The restore drill (PRD-18 RF-9, RNF-3, acceptance criterion 6).
#
# RF-9 asks for a documented monthly restore test, and the reason is in the PRD itself: a
# backup that was never restored is a hypothesis. Documenting a procedure that nobody runs
# produces a document, not a tested backup, so the drill is a command instead.
#
# It exercises the whole path against real containers, with nothing stubbed:
#
#   a database with data  ->  pg_dump  ->  upload to S3 (MinIO)  ->  retention
#                         ->  restore into an EMPTY database  ->  compare row by row
#
# It touches nothing of any real environment: its own network, its own containers, its own
# bucket, all removed on exit. Run it monthly, and after any change to deploy/backup/.
#
#   make backup-drill
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

NETWORK=dizen-backup-drill
IMAGE=dizen-backup-drill
ROWS=500

if ! docker info >/dev/null 2>&1; then
  echo "error: the Docker daemon is not running" >&2
  exit 1
fi

cleanup() {
  docker rm -f drill-src drill-dst drill-minio >/dev/null 2>&1 || true
  docker network rm "$NETWORK" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM
cleanup

step() { printf '\033[1;34m==> %s\033[0m\n' "$1"; }
fail() { printf '\033[31mDRILL FAILED: %s\033[0m\n' "$1" >&2; exit 1; }

step "environment: source database, empty target, object storage"
docker network create "$NETWORK" >/dev/null

for name in drill-src drill-dst; do
  docker run -d --name "$name" --network "$NETWORK" \
    -e POSTGRES_USER=identity -e POSTGRES_PASSWORD=drill -e POSTGRES_DB=identity_db \
    postgres:18.6-alpine >/dev/null
done

docker run -d --name drill-minio --network "$NETWORK" \
  -e MINIO_ROOT_USER=drill -e MINIO_ROOT_PASSWORD=drill12345 \
  minio/minio:latest server /data >/dev/null

for name in drill-src drill-dst; do
  ready=false
  for _ in $(seq 40); do
    if docker exec "$name" pg_isready -U identity -d identity_db >/dev/null 2>&1; then
      ready=true
      break
    fi
    sleep 1
  done
  [ "$ready" = true ] || fail "$name never became ready"
done
sleep 3

step "data to lose: $ROWS rows, a unique constraint and an index"
docker exec drill-src psql -U identity -d identity_db -q -c "
  CREATE TABLE users (
    id         serial primary key,
    email      text not null unique,
    created_at timestamptz default now()
  );
  INSERT INTO users (email)
    SELECT 'user' || g || '@dizen.app' FROM generate_series(1, ${ROWS}) g;
  CREATE INDEX users_email_idx ON users (email);"

source_rows="$(docker exec drill-src psql -U identity -d identity_db -tAc 'SELECT count(*) FROM users')"
# A checksum over the content, not just a row count: a restore that produces the right
# number of wrong rows has to fail this drill.
source_sum="$(docker exec drill-src psql -U identity -d identity_db -tAc \
  "SELECT md5(string_agg(email, ',' ORDER BY id)) FROM users")"
source_indexes="$(docker exec drill-src psql -U identity -d identity_db -tAc \
  "SELECT count(*) FROM pg_indexes WHERE tablename='users'")"
echo "    ${source_rows} rows, ${source_indexes} indexes, checksum ${source_sum:0:12}"

step "building the backup image from deploy/backup"
docker build -q -t "$IMAGE" deploy/backup >/dev/null

# The same variables the compose file sets, pointed at MinIO instead of R2. That is the
# point of configuring rclone from the environment: the drill runs the production code
# path, not a copy of it.
backup_env=(
  -e DIZEN_ENV=drill
  -e BACKUP_BUCKET=dizen-backups
  -e BACKUP_DATABASES=identity
  -e DSN_IDENTITY="postgres://identity:drill@drill-src:5432/identity_db?sslmode=disable"
  -e RCLONE_CONFIG_BACKUP_TYPE=s3
  -e RCLONE_CONFIG_BACKUP_PROVIDER=Minio
  -e RCLONE_CONFIG_BACKUP_ENDPOINT=http://drill-minio:9000
  -e RCLONE_CONFIG_BACKUP_REGION=us-east-1
  -e RCLONE_CONFIG_BACKUP_ACCESS_KEY_ID=drill
  -e RCLONE_CONFIG_BACKUP_SECRET_ACCESS_KEY=drill12345
)

run_backup() { docker run --rm --network "$NETWORK" "${backup_env[@]}" "$IMAGE" "$@"; }
run_rclone() { docker run --rm --network "$NETWORK" "${backup_env[@]}" --entrypoint rclone "$IMAGE" "$@"; }

run_rclone mkdir backup:dizen-backups >/dev/null

step "backup"
run_backup backup | sed 's/^/    /'

target_dsn="postgres://identity:drill@drill-dst:5432/identity_db?sslmode=disable"

step "a restore without --yes must write nothing"
run_backup restore identity "$target_dsn" >/dev/null
tables="$(docker exec drill-dst psql -U identity -d identity_db -tAc \
  "SELECT count(*) FROM information_schema.tables WHERE table_schema='public'")"
[ "$tables" = "0" ] || fail "the dry run wrote $tables tables into the target"
echo "    the target is still empty"

step "restore into the empty database"
run_backup restore identity "$target_dsn" --yes | sed 's/^/    /'

step "compare"
restored_rows="$(docker exec drill-dst psql -U identity -d identity_db -tAc 'SELECT count(*) FROM users')"
restored_sum="$(docker exec drill-dst psql -U identity -d identity_db -tAc \
  "SELECT md5(string_agg(email, ',' ORDER BY id)) FROM users")"
restored_indexes="$(docker exec drill-dst psql -U identity -d identity_db -tAc \
  "SELECT count(*) FROM pg_indexes WHERE tablename='users'")"

[ "$restored_rows" = "$source_rows" ] || fail "$restored_rows rows restored, $source_rows expected"
[ "$restored_sum" = "$source_sum" ] || fail "the content differs from the source"
[ "$restored_indexes" = "$source_indexes" ] || fail "$restored_indexes indexes restored, $source_indexes expected"
echo "    ${restored_rows} rows, ${restored_indexes} indexes, checksum ${restored_sum:0:12}"

step "retention keeps 7 daily"
for day in 01 02 03 04 05 06 07 08 09; do
  docker run --rm --network "$NETWORK" "${backup_env[@]}" --entrypoint sh "$IMAGE" -c \
    "echo drill > /tmp/f && rclone copyto /tmp/f backup:dizen-backups/drill/daily/2026-08-${day}T00-00-00Z/identity.dump" \
    >/dev/null
done

run_backup prune | sed 's/^/    /'

stored() {
  run_rclone lsf --dirs-only "backup:dizen-backups/drill/${1}" \
    | sed 's:/$::' \
    | grep -cE '^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}-[0-9]{2}-[0-9]{2}Z$' || true
}

kept="$(stored daily)"
[ "$kept" = "7" ] || fail "retention kept $kept daily runs instead of 7"

newest="$(run_rclone lsf --dirs-only backup:dizen-backups/drill/daily \
  | sed 's:/$::' | grep -E '^[0-9]{4}' | sort | tail -1)"
echo "    7 kept, and the newest survived: ${newest}"

# Lexicographic order equals chronological order only because the names are fixed-width UTC
# timestamps. A directory that does not look like one is where that assumption breaks, and
# in a destructive operation the cost of being wrong is a deleted backup.
step "a directory this script did not create is never deleted"
docker run --rm --network "$NETWORK" "${backup_env[@]}" --entrypoint sh "$IMAGE" -c \
  "echo drill > /tmp/f && rclone copyto /tmp/f backup:dizen-backups/drill/daily/manual-copy/identity.dump" \
  >/dev/null
run_backup prune >/dev/null
survived="$(run_rclone lsf --dirs-only backup:dizen-backups/drill/daily | sed 's:/$::' | grep -c '^manual-copy$' || true)"
[ "$survived" = "1" ] || fail "prune deleted a directory it did not create"
echo "    it survived"

step "a retention of zero is refused"
if run_backup prune 0 4 3 >/dev/null 2>&1; then
  fail "prune accepted a retention of 0"
fi
still="$(stored daily)"
[ "$still" = "7" ] || fail "prune deleted something despite refusing"
echo "    refused, and nothing was deleted"

printf '\n\033[32mDRILL PASSED\033[0m: %s rows and %s indexes restored identically, retention verified\n\n' \
  "$source_rows" "$source_indexes"
