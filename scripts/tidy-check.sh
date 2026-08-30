#!/usr/bin/env bash
# Verifies that every go.mod and go.sum in the workspace is tidy (hard rule 3, same idea).
#
# `go mod tidy` is not cosmetic here. A direct dependency left marked `// indirect` is a
# manifest that lies about what the module actually imports, and nothing fails because of
# it: the build works, the tests pass, and the drift only surfaces the day an editor points
# at it. That is exactly how this check came to exist.
#
# It runs `make tidy` and fails on any diff, which is the same contract as proto-check and
# sqlc-check: the committed state has to be the generated one. Note that `make tidy` is not
# `go mod tidy` everywhere -- see the comment on that target -- because tidy ignores the
# workspace and the service modules cannot be resolved without it.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if ! git rev-parse --git-dir >/dev/null 2>&1; then
  echo "error: tidy-check requires an initialized git repository" >&2
  exit 1
fi

make --no-print-directory tidy

DIFF="$(git status --porcelain -- '*go.mod' '*go.sum')"

if [ -n "$DIFF" ]; then
  echo "" >&2
  echo "error: 'make tidy' left uncommitted changes." >&2
  echo "       Run 'make tidy' and commit the result." >&2
  echo "" >&2
  echo "$DIFF" >&2
  echo "" >&2
  git --no-pager diff -- '*go.mod' >&2 || true
  exit 1
fi

echo "==> every go.mod and go.sum is tidy"
