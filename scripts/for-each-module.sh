#!/usr/bin/env bash
# Runs a command in every module of the go.work workspace.
#
# `go build ./...` from the repository root does not cross module boundaries (RF-1,
# decision D-1): in a multi-module workspace the ./... pattern only reaches the current
# module. This script fills that gap and backs the `build`, `test` and `vet` targets.
#
# Usage: scripts/for-each-module.sh go build ./...
#
# Compatible with bash 3.2 (the version macOS ships): no `mapfile`, no associative arrays.
set -euo pipefail

if [ $# -eq 0 ]; then
  echo "usage: $0 <command> [args...]" >&2
  exit 2
fi

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

# In workspace mode `go list -m` enumerates the modules declared in go.work.
MODULES="$(go list -m -f '{{.Dir}}')"

if [ -z "$MODULES" ]; then
  echo "error: no modules found in the workspace" >&2
  exit 1
fi

STATUS=0
while IFS= read -r dir; do
  [ -n "$dir" ] || continue

  # The root module used to be skipped as empty. It is not: tools/apicheck lives in it, and
  # skipping it meant that package was never vetted, never linted and never tidied -- which
  # is how a stale `// indirect` in the root go.mod survived until an editor pointed at it.
  if [ "$dir" = "$ROOT" ]; then
    rel="root"
  else
    rel="${dir#"$ROOT"/}"
  fi
  printf '\033[1;34m==> %s\033[0m\n' "$rel"

  # The module-relative path is exported so the command can keep its artifacts apart per
  # module; without it `go build -o` would overwrite binaries across services, because
  # every service has its main package in cmd/server.
  export MODULE_REL="$rel"

  if ! ( cd "$dir" && "$@" ); then
    STATUS=1
  fi
done <<EOF_MODULES
$MODULES
EOF_MODULES

exit $STATUS
