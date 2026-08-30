#!/usr/bin/env sh
# Daily backup of every database to object storage (PRD-18 RF-9, RNF-3).
#
# One run produces one dated directory holding one dump per database plus a manifest, and
# uploads it off the server. Everything is in one directory because a restore needs the
# whole set from the same moment: five dumps taken minutes apart are five different points
# in time, and reconciling them afterwards is not something anybody wants to do during an
# incident.
#
# Retention is 7 daily, 4 weekly and 3 monthly (RF-9). The weekly and monthly copies are
# made server-side from the daily one, so a promotion costs no second dump and no egress.
#
#   RPO <= 24 h  because it runs daily
#   RTO <= 2 h   because restore.sh restores a directory without any manual step
set -eu

: "${BACKUP_DATABASES:?the list of databases to back up is required}"
: "${BACKUP_BUCKET:?the destination bucket is required}"
: "${DIZEN_ENV:?the environment name is required}"

RETAIN_DAILY="${BACKUP_RETAIN_DAILY:-7}"
RETAIN_WEEKLY="${BACKUP_RETAIN_WEEKLY:-4}"
RETAIN_MONTHLY="${BACKUP_RETAIN_MONTHLY:-3}"

REMOTE="backup:${BACKUP_BUCKET}/${DIZEN_ENV}"

STAMP="$(date -u +%Y-%m-%dT%H-%M-%SZ)"
DAY_OF_WEEK="$(date -u +%u)"    # 1 = Monday ... 7 = Sunday
DAY_OF_MONTH="$(date -u +%d)"

WORK="/tmp/backup/${STAMP}"
mkdir -p "$WORK"

# The scratch directory goes away whatever happens: a failed run must not leave a partial
# dump filling the disk of the server it was protecting.
cleanup() { rm -rf "$WORK"; }
trap cleanup EXIT INT TERM

log() { echo "[$(date -u +%H:%M:%S)] $*"; }

log "backup ${STAMP} (${DIZEN_ENV})"

FAILED=""

for name in $BACKUP_DATABASES; do
  # Each database has its own credential, so each has its own variable: DSN_IDENTITY,
  # DSN_TOURS, and so on (hard rule 2).
  var="DSN_$(echo "$name" | tr '[:lower:]-' '[:upper:]_')"
  dsn="$(eval "printf '%s' \"\${${var}:-}\"")"

  if [ -z "$dsn" ]; then
    log "  ${name}: FAILED, ${var} is not set"
    FAILED="${FAILED} ${name}"
    continue
  fi

  file="${WORK}/${name}.dump"

  # -Fc is the custom format: compressed, and it can be listed and restored selectively,
  # which is what makes the verification below possible. A plain SQL file cannot be checked
  # for integrity without replaying it.
  if ! pg_dump --format=custom --compress=6 --no-owner --no-privileges \
        --file="$file" "$dsn" 2>"${WORK}/${name}.err"; then
    log "  ${name}: FAILED to dump"
    sed 's/^/      /' "${WORK}/${name}.err" || true
    FAILED="${FAILED} ${name}"
    continue
  fi

  # A dump that cannot be listed cannot be restored. Checking it here costs a second and
  # turns "the backup ran" into "the backup is readable", which are not the same claim.
  if ! pg_restore --list "$file" > /dev/null 2>&1; then
    log "  ${name}: FAILED, the dump is not readable"
    FAILED="${FAILED} ${name}"
    continue
  fi

  size="$(du -h "$file" | cut -f1)"
  log "  ${name}: ${size}"
done

rm -f "${WORK}"/*.err

if [ -n "$FAILED" ]; then
  log "backup FAILED for:${FAILED}"
  log "nothing was uploaded: a partial set is worse than none, because it looks like a backup"
  exit 1
fi

# The manifest is what a restore reads first, and what tells whoever finds this directory in
# six months what it is and what produced it.
cat > "${WORK}/MANIFEST" <<MANIFEST
environment: ${DIZEN_ENV}
taken_at: $(date -u +%Y-%m-%dT%H:%M:%SZ)
databases: ${BACKUP_DATABASES}
pg_dump: $(pg_dump --version)
format: custom (pg_restore)
restore: docker compose run --rm backup restore <database> <target-dsn> --run ${STAMP} --yes
MANIFEST

log "uploading to ${REMOTE}/daily/${STAMP}"
rclone copy "$WORK" "${REMOTE}/daily/${STAMP}" --checksum

# Sunday and the first of the month promote the same directory instead of dumping again.
if [ "$DAY_OF_WEEK" = "7" ]; then
  log "promoting to weekly"
  rclone copy "${REMOTE}/daily/${STAMP}" "${REMOTE}/weekly/${STAMP}"
fi

if [ "$DAY_OF_MONTH" = "01" ]; then
  log "promoting to monthly"
  rclone copy "${REMOTE}/daily/${STAMP}" "${REMOTE}/monthly/${STAMP}"
fi

/usr/local/bin/prune.sh "$RETAIN_DAILY" "$RETAIN_WEEKLY" "$RETAIN_MONTHLY"

# A marker with the time of the last successful run. RF-15 alerts on a failed backup, and
# what it will watch is this object not moving, which also catches the run that never
# started -- a failure no exit code can report.
echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) ${STAMP}" > "/tmp/backup/LAST_SUCCESS"
rclone copyto "/tmp/backup/LAST_SUCCESS" "${REMOTE}/LAST_SUCCESS"

log "backup ${STAMP} done"
