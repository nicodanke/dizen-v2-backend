#!/usr/bin/env sh
# Restores one database from a backup (PRD-18 RF-9, RNF-3, acceptance criterion 6).
#
#   restore.sh                                list what can be restored
#   restore.sh <database> <target-dsn>        show what would be restored, and stop
#   restore.sh <database> <target-dsn> --yes  restore it
#
#   restore.sh identity "$DSN" --run 2026-08-30T03-00-00Z --prefix weekly --yes
#
# The default is the newest daily run. Without --yes nothing is written: this command drops
# and recreates every object in the target database, and the one situation where it gets run
# is an incident, which is exactly when a typo is most likely.
#
# The restore drill of RF-9 is this command against an empty database, once a month, with
# the result written down. A backup that has never been restored is a hypothesis.
set -eu

: "${BACKUP_BUCKET:?the destination bucket is required}"
: "${DIZEN_ENV:?the environment name is required}"

REMOTE="backup:${BACKUP_BUCKET}/${DIZEN_ENV}"

DATABASE="${1:-}"
TARGET_DSN="${2:-}"
PREFIX="daily"
RUN=""
CONFIRMED=false

[ $# -ge 2 ] && shift 2 || shift $# 2>/dev/null || true

while [ $# -gt 0 ]; do
  case "$1" in
    --yes)    CONFIRMED=true ;;
    --run)    RUN="${2:-}"; shift ;;
    --prefix) PREFIX="${2:-}"; shift ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
  shift
done

list_runs() {
  for prefix in daily weekly monthly; do
    echo "==> ${prefix}"
    rclone lsf --dirs-only "${REMOTE}/${prefix}" 2>/dev/null | sed 's:/$::' | sort | sed 's/^/    /' \
      || echo "    (empty)"
  done
}

if [ -z "$DATABASE" ] || [ -z "$TARGET_DSN" ]; then
  echo "Available backups in ${REMOTE}:"
  echo ""
  list_runs
  echo ""
  echo "usage: restore.sh <database> <target-dsn> [--run <stamp>] [--prefix daily|weekly|monthly] [--yes]"
  exit 0
fi

if [ -z "$RUN" ]; then
  RUN="$(rclone lsf --dirs-only "${REMOTE}/${PREFIX}" 2>/dev/null | sed 's:/$::' | sort | tail -1)"
fi

if [ -z "$RUN" ]; then
  echo "error: there is no backup under ${REMOTE}/${PREFIX}" >&2
  exit 1
fi

SOURCE="${REMOTE}/${PREFIX}/${RUN}/${DATABASE}.dump"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT INT TERM

echo "==> ${DATABASE} from ${PREFIX}/${RUN}"

if ! rclone copyto "$SOURCE" "${WORK}/${DATABASE}.dump" 2>/dev/null; then
  echo "error: ${SOURCE} does not exist" >&2
  echo "" >&2
  list_runs >&2
  exit 1
fi

# The manifest travels with the dump and says when the set was taken. Reading it before
# restoring is how the person running this finds out they are about to restore Tuesday.
rclone cat "${REMOTE}/${PREFIX}/${RUN}/MANIFEST" 2>/dev/null | sed 's/^/    /' || true

pg_restore --list "${WORK}/${DATABASE}.dump" > /dev/null
echo "    the dump is readable"

if [ "$CONFIRMED" != "true" ]; then
  echo ""
  echo "Nothing was written. This would DROP and recreate every object in the target"
  echo "database. Add --yes to go ahead."
  exit 0
fi

echo "    restoring into the target database"

# --clean --if-exists makes the restore repeatable: a second run over a database that is
# already populated replaces it instead of colliding with every object.
#
# --no-owner and --no-privileges because the roles of the destination are not those of the
# source; the target database belongs to whoever the target DSN connects as. It is also what
# lets a production dump be restored into a scratch database during the drill.
#
# The exit code is not checked with `set -e` alone: pg_restore reports non-fatal warnings as
# a failure, and an extension that already exists is one of them.
if pg_restore --clean --if-exists --no-owner --no-privileges \
     --dbname="$TARGET_DSN" "${WORK}/${DATABASE}.dump"; then
  echo "==> ${DATABASE} restored from ${PREFIX}/${RUN}"
else
  echo ""
  echo "pg_restore reported errors. They are often benign (an object that did not exist to"
  echo "drop, an extension already present), but they are not assumed to be: check the"
  echo "output above and verify the data before declaring the restore good." >&2
  exit 1
fi
