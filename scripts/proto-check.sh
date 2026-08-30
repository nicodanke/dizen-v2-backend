#!/usr/bin/env bash
# Verifies that the committed generated code is up to date (hard rule 3).
#
# It regenerates everything and fails if a diff is left behind, which means somebody
# touched a .proto without running `make proto`, or bumped a plugin version without
# regenerating. This is the same command CI runs.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if ! git rev-parse --git-dir >/dev/null 2>&1; then
  echo "error: proto-check requires an initialized git repository" >&2
  exit 1
fi

make --no-print-directory proto

# pubspec.lock and .dart_tool are not versioned; the rest of the generated tree is.
DIFF="$(git status --porcelain -- pkg/genproto gen/openapi gen/dart)"

if [ -n "$DIFF" ]; then
  echo "" >&2
  echo "error: 'make proto' left uncommitted changes." >&2
  echo "       Generated code is versioned: run 'make proto' and commit the result." >&2
  echo "" >&2
  echo "$DIFF" >&2
  echo "" >&2
  git --no-pager diff --stat -- pkg/genproto gen/openapi gen/dart >&2 || true
  exit 1
fi

echo "==> generated code is up to date"
