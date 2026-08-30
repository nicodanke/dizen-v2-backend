#!/usr/bin/env sh
# Entry point of the backup container (PRD-18 RF-9).
#
#   schedule    install the crontab and stay in the foreground (the default)
#   backup      run one backup now and exit
#   restore     restore a dump; see restore.sh
#   prune       apply the retention policy now and exit
#   verify      list what is stored, per prefix
set -eu

COMMAND="${1:-schedule}"
shift 2>/dev/null || true

case "$COMMAND" in
  backup)  exec /usr/local/bin/backup.sh "$@" ;;
  restore) exec /usr/local/bin/restore.sh "$@" ;;
  prune)   exec /usr/local/bin/prune.sh "$@" ;;

  verify)
    for prefix in daily weekly monthly; do
      echo "==> $prefix"
      rclone lsl "backup:${BACKUP_BUCKET}/${DIZEN_ENV}/${prefix}" 2>/dev/null || echo "    (empty)"
    done
    ;;

  schedule)
    SCHEDULE="${BACKUP_SCHEDULE:-0 3 * * *}"

    # The environment of the container does not reach a cron job, so it is dumped to a file
    # that the job sources. Without this the job runs with an empty environment and fails
    # every night in a way nobody notices until a restore is needed.
    export | sed 's/^export //' > /etc/backup.env

    echo "$SCHEDULE . /etc/backup.env; /usr/local/bin/backup.sh >> /proc/1/fd/1 2>&1" > /etc/crontabs/root

    echo "backup scheduled: $SCHEDULE (TZ=${TZ:-UTC})"
    echo "databases: ${BACKUP_DATABASES}"
    echo "destination: backup:${BACKUP_BUCKET}/${DIZEN_ENV}"

    # -f keeps crond in the foreground so the container lives; -d 8 sends its log to stderr,
    # where Docker collects it.
    exec crond -f -d 8
    ;;

  *)
    echo "unknown command: $COMMAND" >&2
    echo "expected one of: schedule backup restore prune verify" >&2
    exit 2
    ;;
esac
