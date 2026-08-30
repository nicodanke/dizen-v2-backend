#!/usr/bin/env bash
# Runs golang-migrate against one service's database.
#
# Usage: scripts/migrate.sh <up|down|create|version|force> <service> [args...]
#
# In production migrations are applied at container startup (RF-7), never through this
# script: it is a development and emergency tool.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
MIGRATE="$ROOT/bin/migrate"

COMMAND="${1:-}"
SERVICE="${2:-}"

if [ -z "$COMMAND" ] || [ -z "$SERVICE" ]; then
  echo "usage: $0 <up|down|create|version|force> <service> [args...]" >&2
  exit 2
fi

MIGRATIONS_DIR="$ROOT/services/$SERVICE/internal/db/migrations"

if [ ! -d "$MIGRATIONS_DIR" ]; then
  echo "error: unknown service '$SERVICE' (no $MIGRATIONS_DIR)" >&2
  exit 1
fi

# `create` needs no database, so it is handled before the DSN is resolved.
if [ "$COMMAND" = "create" ]; then
  NAME="${3:-}"

  if [ -z "$NAME" ]; then
    echo "usage: $0 create <service> <migration_name>" >&2
    exit 2
  fi

  "$MIGRATE" create -ext sql -dir "$MIGRATIONS_DIR" -seq "$NAME"
  exit 0
fi

# The DSN comes from the environment, or from the service's .env, so a developer never has
# to paste a connection string into a command.
if [ -z "${DATABASE_URL:-}" ] && [ -f "$ROOT/services/$SERVICE/.env" ]; then
  # shellcheck disable=SC1090
  set -a && . "$ROOT/services/$SERVICE/.env" && set +a
fi

if [ -z "${DATABASE_URL:-}" ]; then
  echo "error: DATABASE_URL is not set and services/$SERVICE/.env does not define it" >&2
  exit 1
fi

exec "$MIGRATE" -path "$MIGRATIONS_DIR" -database "$DATABASE_URL" "$COMMAND" "${@:3}"
