#!/usr/bin/env bash
# Prints the api-vX.Y.Z tag that the next contract publication should carry.
#
# The contract is versioned independently of the services (01 section 3.1): `v1.4.0` is a
# release of this backend, `api-v1.4.0` is a state of the API that dizen-v2-mobile and
# dizen-v2-web pin themselves to. They move at different rhythms and must not share a
# number.
#
# The default bump is the minor one, because that is what the rules make almost every change
# be: proto changes are additive (hard rule 4), and an added field or RPC is a new minor.
# The other two are explicit, written as a trailer in the commit that reaches main:
#
#   api-release: major     a v2 package that coexists with v1 (01 section 3.2)
#   api-release: patch     a comment, a rename in a reserved range, nothing a client sees
#
# Usage: scripts/next-api-version.sh [major|minor|patch]
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

FIRST_VERSION="api-v0.1.0"

bump="${1:-}"

if [ -z "$bump" ]; then
  # `git log -1` rather than the environment: the script has to give the same answer when
  # somebody runs it locally to see what the next tag would be.
  message="$(git log -1 --pretty=%B)"

  if grep -qiE '^[[:space:]]*api-release:[[:space:]]*major[[:space:]]*$' <<<"$message"; then
    bump=major
  elif grep -qiE '^[[:space:]]*api-release:[[:space:]]*patch[[:space:]]*$' <<<"$message"; then
    bump=patch
  else
    bump=minor
  fi
fi

case "$bump" in
  major|minor|patch) ;;
  *)
    echo "error: unknown bump '$bump'; expected major, minor or patch" >&2
    exit 2
    ;;
esac

latest="$(git tag -l 'api-v*' --sort=-v:refname | head -1)"

if [ -z "$latest" ]; then
  echo "$FIRST_VERSION"
  exit 0
fi

if [[ ! "$latest" =~ ^api-v([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]; then
  echo "error: the latest contract tag '$latest' is not api-vX.Y.Z" >&2
  exit 1
fi

major="${BASH_REMATCH[1]}"
minor="${BASH_REMATCH[2]}"
patch="${BASH_REMATCH[3]}"

case "$bump" in
  major) major=$((major + 1)); minor=0; patch=0 ;;
  minor) minor=$((minor + 1)); patch=0 ;;
  patch) patch=$((patch + 1)) ;;
esac

echo "api-v${major}.${minor}.${patch}"
