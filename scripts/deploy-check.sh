#!/usr/bin/env bash
# Validates deploy/docker-compose.prod.yml (PRD-18 RF-3).
#
# Two things, and the second is the one that matters:
#
#   1. the file parses and every service, network and label is well formed;
#   2. every ${VARIABLE} it reads is named in deploy/dokploy.env.example.
#
# Compose only warns about an unset variable and carries on with an empty string, which is
# how a deployment ends up with an empty database password or a router with no host. The
# warning is turned into a failure here so that adding a variable to the compose file and
# forgetting to document it is caught in CI and not on the server.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

COMPOSE_FILE="deploy/docker-compose.prod.yml"
ENV_FILE="deploy/dokploy.env.example"

if ! docker compose version >/dev/null 2>&1; then
  echo "error: the docker compose plugin is not available" >&2
  exit 1
fi

STDERR="$(mktemp)"
trap 'rm -f "$STDERR"' EXIT

# The example file carries every variable with an empty value, which is enough to resolve
# the interpolation: what is being checked here is the shape of the file, not the values.
if ! docker compose -f "$COMPOSE_FILE" --env-file "$ENV_FILE" config --quiet 2>"$STDERR"; then
  echo "error: $COMPOSE_FILE is not valid" >&2
  cat "$STDERR" >&2
  exit 1
fi

if grep -q 'variable is not set' "$STDERR"; then
  echo "" >&2
  echo "error: $COMPOSE_FILE reads variables that $ENV_FILE does not document:" >&2
  echo "" >&2
  # Compose wraps the message in its own logging format, so the name arrives quoted and
  # the quotes arrive escaped: msg="The \"NAME\" variable is not set". The optional
  # backslash keeps this working on the versions that print it plainly.
  grep -oE '[\\]?"[A-Za-z_][A-Za-z0-9_]*[\\]?" variable is not set' "$STDERR" \
    | sed -e 's/ variable is not set//' -e 's/[\\"]//g' \
    | sort -u \
    | sed 's/^/    /' >&2
  echo "" >&2
  echo "       Add them to $ENV_FILE, empty and commented (hard rule 6)." >&2
  exit 1
fi

# Both profiles too: a service that only starts in staging still has to be valid.
docker compose -f "$COMPOSE_FILE" --env-file "$ENV_FILE" \
  --profile authoring --profile tiles config --quiet 2>>"$STDERR"

echo "==> $COMPOSE_FILE is valid and every variable it reads is documented"
