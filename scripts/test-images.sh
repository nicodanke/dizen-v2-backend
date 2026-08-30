#!/usr/bin/env bash
# Prints the container images the integration tests run (PRD-18 RF-11).
#
# The list is read from pkg/testutils rather than written down here, because two lists drift:
# the tests would pull one Postgres and start another, and the pin that keeps the tests
# honest -- the images match deploy/docker-compose.yml -- would only be true in one of them.
#
# CI uses it to pull the images in parallel before the tests, so a slow registry shows up as
# a slow pull rather than as a slow test suite.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

SOURCE="pkg/testutils/containers.go"

if [ ! -f "$SOURCE" ]; then
  echo "error: $SOURCE does not exist" >&2
  exit 1
fi

IMAGES="$(sed -nE 's/^[[:space:]]*[A-Za-z]+Image[[:space:]]*=[[:space:]]*"([^"]+)".*/\1/p' "$SOURCE")"

COUNT="$(printf '%s\n' "$IMAGES" | grep -c . || true)"

# Four today: Postgres, PostGIS, Redis and RabbitMQ. Finding fewer means the constants were
# renamed and this script is silently returning an incomplete list, which is worse than
# returning none: CI would pull nothing and report success.
if [ "$COUNT" -lt 4 ]; then
  echo "error: found $COUNT images in $SOURCE, expected at least 4" >&2
  echo "       the *Image constants were probably renamed; update this script" >&2
  exit 1
fi

printf '%s\n' "$IMAGES"
