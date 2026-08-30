#!/usr/bin/env bash
# Scans the repository for committed secrets (PRD-18 RF-11 and RNF-4).
#
# Two passes, because they catch different mistakes:
#
#   dir  the working tree as it stands. This is what fails when a secret is sitting in a
#        file right now, which is the common case.
#   git  every commit reachable from HEAD. This is what fails when a secret was added and
#        then removed in a later commit of the same branch: the file is clean, the history
#        is not, and the secret is just as leaked. It is the case acceptance criterion 8
#        of PRD-18 describes.
#
# The history pass needs the full history. In CI that means `fetch-depth: 0`; a shallow
# clone scans only what it has and passes for the wrong reason, so this warns when it sees
# one instead of pretending the check ran.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

GITLEAKS="$ROOT/bin/gitleaks"

if [ ! -x "$GITLEAKS" ]; then
  echo "error: gitleaks is not installed in ./bin" >&2
  echo "       run 'make tools' (the version is pinned in tools/versions.mk)" >&2
  exit 1
fi

if ! git rev-parse --git-dir >/dev/null 2>&1; then
  echo "error: secrets-scan requires an initialized git repository" >&2
  exit 1
fi

# --redact keeps the finding out of the log: a CI log is one more place a secret should
# not end up, and the report says which file and which rule, which is what is needed to
# fix it.
COMMON=(--no-banner --redact --config "$ROOT/.gitleaks.toml")

echo "==> working tree"
"$GITLEAKS" dir . "${COMMON[@]}"

if [ "$(git rev-parse --is-shallow-repository)" = "true" ]; then
  echo ""
  echo "warning: the clone is shallow, so the history was not scanned in full." >&2
  echo "         In CI this means actions/checkout is missing 'fetch-depth: 0'." >&2
fi

echo ""
echo "==> history"
"$GITLEAKS" git . "${COMMON[@]}"
