#!/usr/bin/env bash
# Verifies that the committed sqlc output is up to date (hard rule 3).
#
# Same idea as proto-check: regenerate and fail on any diff, so a query changed without
# regenerating is caught in CI instead of at runtime.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if ! git rev-parse --git-dir >/dev/null 2>&1; then
  echo "error: sqlc-check requires an initialized git repository" >&2
  exit 1
fi

make --no-print-directory sqlc

DIFF="$(git status --porcelain -- 'services/*/internal/db/sqlc')"

if [ -n "$DIFF" ]; then
  echo "" >&2
  echo "error: 'make sqlc' left uncommitted changes." >&2
  echo "       Generated code is versioned: run 'make sqlc' and commit the result." >&2
  echo "" >&2
  echo "$DIFF" >&2
  exit 1
fi

echo "==> the sqlc output is up to date"
