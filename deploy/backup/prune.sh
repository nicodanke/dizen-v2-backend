#!/usr/bin/env sh
# Applies the retention policy (PRD-18 RF-9): 7 daily, 4 weekly, 3 monthly.
#
# Run directories are named after a UTC timestamp, so sorting them lexicographically sorts
# them chronologically, and keeping the N last is keeping the N newest.
#
#   prune.sh [daily] [weekly] [monthly]
set -eu

: "${BACKUP_BUCKET:?the destination bucket is required}"
: "${DIZEN_ENV:?the environment name is required}"

REMOTE="backup:${BACKUP_BUCKET}/${DIZEN_ENV}"

DAILY="${1:-${BACKUP_RETAIN_DAILY:-7}}"
WEEKLY="${2:-${BACKUP_RETAIN_WEEKLY:-4}}"
MONTHLY="${3:-${BACKUP_RETAIN_MONTHLY:-3}}"

prune_prefix() {
  prefix="$1"
  keep="$2"

  # A retention of zero would delete everything on the first run. It is refused rather than
  # obeyed: nothing about this script should be able to empty the backups.
  if [ "$keep" -lt 1 ]; then
    echo "refusing to prune ${prefix} with a retention of ${keep}" >&2
    return 1
  fi

  # Only directories this script produced are considered, and the filter is not paranoia.
  # The ordering here is lexicographic, which equals chronological *because* the names are
  # fixed-width UTC timestamps. Anything else -- a hand-uploaded directory, a name with an
  # unpadded day -- sorts wherever its characters fall, and in a destructive operation that
  # means deleting the newest backup while keeping a stale one. Names that do not match are
  # left alone and reported, never deleted and never counted.
  all="$(rclone lsf --dirs-only "${REMOTE}/${prefix}" 2>/dev/null | sed 's:/$::' || true)"

  [ -z "$all" ] && return 0

  pattern='^[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]-[0-9][0-9]-[0-9][0-9]Z$'

  runs="$(echo "$all" | grep -E "$pattern" | sort || true)"
  foreign="$(echo "$all" | grep -vE "$pattern" || true)"

  if [ -n "$foreign" ]; then
    echo "  ${prefix}: ignoring entries this script did not create:"
    echo "$foreign" | sed 's/^/      /'
  fi

  [ -z "$runs" ] && return 0

  total="$(echo "$runs" | wc -l | tr -d ' ')"
  [ "$total" -le "$keep" ] && return 0

  drop="$((total - keep))"

  echo "  ${prefix}: ${total} stored, keeping ${keep}, removing ${drop}"

  echo "$runs" | head -n "$drop" | while IFS= read -r run; do
    [ -n "$run" ] || continue
    echo "    - ${run}"
    rclone purge "${REMOTE}/${prefix}/${run}"
  done
}

echo "applying retention (${DAILY} daily, ${WEEKLY} weekly, ${MONTHLY} monthly)"
prune_prefix daily "$DAILY"
prune_prefix weekly "$WEEKLY"
prune_prefix monthly "$MONTHLY"
