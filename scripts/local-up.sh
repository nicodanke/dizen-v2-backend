#!/usr/bin/env bash
# Brings up the local environment, with secrets from Doppler when it is configured (D-33).
#
# The point of Doppler locally is not the compose passwords -- those are `dizen`/`dizen` on
# throwaway containers and are in the file for everyone to read. It is the one real secret a
# developer machine ends up holding: the JWT signing key, which `make jwt-key` otherwise
# writes to deploy/.env. With Doppler there is no key file on any laptop.
#
# It degrades on purpose: a fresh clone without Doppler still gets a working environment from
# the committed defaults, because an onboarding step that blocks `make up` is a step people
# work around.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

COMPOSE=(docker compose -f "$ROOT/deploy/docker-compose.yml")

# `doppler configure get` reads the local configuration and makes no network call, so this
# costs nothing on a machine that does not use it.
if command -v doppler >/dev/null 2>&1 && doppler configure get project --plain >/dev/null 2>&1; then
  project="$(doppler configure get project --plain)"
  config="$(doppler configure get config --plain 2>/dev/null || echo '?')"

  echo "==> secrets from Doppler (${project}/${config})"

  doppler run -- "${COMPOSE[@]}" up -d --build
else
  echo "==> using the development defaults in the compose file"
  echo "    Doppler is not configured here. To use the shared development secrets:"
  echo "      brew install dopplerhq/cli/doppler && doppler login && doppler setup"
  echo ""

  "${COMPOSE[@]}" up -d --build
fi

"$ROOT/scripts/wait-for-services.sh"
