#!/usr/bin/env bash
# Checks the commit messages about to be pushed (PRD-25 RF-3).
#
# The range is everything not yet on main, which is what a pull request will eventually put
# under the same check in CI. `origin/main` is used as it stands, without fetching: a stale
# ref only makes the range wider, and every commit in it has to pass anyway. Fetching would
# make a push wait on the network for no gain.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if ! git rev-parse --verify --quiet origin/main >/dev/null; then
  echo "==> no origin/main to compare against; skipping the commit message check"
  exit 0
fi

exec make --no-print-directory commit-check RANGE="origin/main..HEAD"
