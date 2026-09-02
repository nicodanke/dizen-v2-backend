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

# Every stack deployed to the server, each with the file that documents its variables. The
# data stores live in their own compose files (D-32), and they drift just as easily as the
# application one.
STACKS=(
  "deploy/docker-compose.prod.yml:deploy/dokploy.env.example"
  "deploy/rabbitmq/docker-compose.yml:deploy/rabbitmq/rabbitmq.env.example"
  "deploy/redis/docker-compose.yml:deploy/redis/redis.env.example"
)

if ! docker compose version >/dev/null 2>&1; then
  echo "error: the docker compose plugin is not available" >&2
  exit 1
fi

STDERR="$(mktemp)"
trap 'rm -f "$STDERR"' EXIT

for stack in "${STACKS[@]}"; do
  compose_file="${stack%%:*}"
  env_file="${stack##*:}"

  : > "$STDERR"

  # Variables declared `${NAME:?...}` are the ones a deployment must not start without, and
  # they are exactly the ones the example file leaves EMPTY -- deliberately, so that copying
  # it wholesale into a real environment fails loudly instead of running with a placeholder
  # password that works.
  #
  # So the check supplies a value for them here, read out of the compose file itself rather
  # than from a list that would drift: what is being verified is the shape of the file, not
  # the values.
  required="$(grep -oE '\$\{[A-Z0-9_]+:\?' "$compose_file" | sed 's/\${//;s/:?//' | sort -u)"

  placeholders=()
  for name in $required; do
    placeholders+=("${name}=deploy-check")
  done

  if ! env "${placeholders[@]}" \
      docker compose -f "$compose_file" --env-file "$env_file" config --quiet 2>"$STDERR"; then
    echo "error: $compose_file is not valid" >&2
    cat "$STDERR" >&2
    exit 1
  fi

  if grep -q 'variable is not set' "$STDERR"; then
    echo "" >&2
    echo "error: $compose_file reads variables that $env_file does not document:" >&2
    echo "" >&2
    # Compose wraps the message in its own logging format, so the name arrives quoted and
    # the quotes arrive escaped: msg="The \"NAME\" variable is not set". The optional
    # backslash keeps this working on the versions that print it plainly.
    grep -oE '[\\]?"[A-Za-z_][A-Za-z0-9_]*[\\]?" variable is not set' "$STDERR" \
      | sed -e 's/ variable is not set//' -e 's/[\\"]//g' \
      | sort -u \
      | sed 's/^/    /' >&2
    echo "" >&2
    echo "       Add them to $env_file, empty and commented (hard rule 6)." >&2
    exit 1
  fi

  echo "==> $compose_file is valid and every variable it reads is documented"
done

# The profiles of the application stack too: a service that only starts in staging still has
# to be valid.
env DIZEN_ENV=deploy-check \
  docker compose -f "deploy/docker-compose.prod.yml" --env-file "deploy/dokploy.env.example" \
    --profile authoring --profile tiles config --quiet
