#!/usr/bin/env bash
# Measures coverage across the whole repository and enforces the 70% gate (RF-18b, RNF-2).
#
# Two things make this more than `go test -cover`:
#
#   -coverpkg=./...  counts the coverage that a test in one package gives to another. Without
#                    it, an integration test that drives a handler through the transport gets
#                    no credit for the repository code it exercised, and the number reads far
#                    lower than the truth.
#   -tags=integration includes the tests that need Docker, which is where most of the
#                    repository and transport coverage comes from.
#
# Generated code is excluded from the total: it is committed (hard rule 3) but writing tests
# for it would measure the generator, not this repository.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

THRESHOLD="${COVERAGE_THRESHOLD:-70}"
PROFILE="$ROOT/coverage.out"
FILTERED="$ROOT/coverage.filtered.out"

# What is excluded, and why:
#   pkg/genproto, gen/       protobuf output
#   internal/db/sqlc/        sqlc output
#   mocks/                   mockery output
#   *.pb.go, *.pb.gw.go      protobuf and gateway output
#   cmd/server/main.go       pure composition; exercised by `make up`, not by a unit test
#   tools/                   build-time helpers, not shipped
EXCLUDE='pkg/genproto/|/gen/|/internal/db/sqlc/|/mocks/|\.pb\.go:|\.pb\.gw\.go:|/cmd/server/main\.go:|/tools/'

TAGS="${COVERAGE_TAGS:-integration}"

echo "==> running the tests with coverage (tags: ${TAGS:-none})"

# One profile per module, merged afterwards: -coverpkg cannot span modules, so each module is
# measured against its own packages plus pkg.
: > "$PROFILE.tmp"
FIRST=1

while IFS= read -r dir; do
  [ -n "$dir" ] || continue
  [ "$dir" = "$ROOT" ] && continue

  rel="${dir#"$ROOT"/}"
  module_profile="$(mktemp)"

  printf '\033[1;34m==> %s\033[0m\n' "$rel"

  if ! ( cd "$dir" && go test \
      ${TAGS:+-tags="$TAGS"} \
      -coverpkg=./... \
      -covermode=atomic \
      -coverprofile="$module_profile" \
      -timeout=30m \
      ./... ); then
    rm -f "$module_profile"
    echo "" >&2
    echo "error: the tests failed; coverage was not measured" >&2
    exit 1
  fi

  if [ -s "$module_profile" ]; then
    if [ "$FIRST" = "1" ]; then
      cat "$module_profile" >> "$PROFILE.tmp"
      FIRST=0
    else
      # The mode line appears once per profile; only the first is kept.
      tail -n +2 "$module_profile" >> "$PROFILE.tmp"
    fi
  fi

  rm -f "$module_profile"
done < <(go list -m -f '{{.Dir}}')

mv "$PROFILE.tmp" "$PROFILE"

# Apply the exclusions, keeping the mode line.
head -1 "$PROFILE" > "$FILTERED"
tail -n +2 "$PROFILE" | grep -vE "$EXCLUDE" >> "$FILTERED" || true

TOTAL="$(go tool cover -func="$FILTERED" | tail -1 | awk '{print $3}' | tr -d '%')"

echo ""
echo "==> coverage by package (excluding generated code)"
go tool cover -func="$FILTERED" \
  | grep -v '^total:' \
  | awk -F: '{print $1}' \
  | sed "s|github.com/nicodanke/dizen-v2-backend/||" \
  | sed 's|/[^/]*\.go$||' \
  | sort -u \
  | while IFS= read -r pkg; do
      pct="$(go tool cover -func="$FILTERED" \
        | grep "dizen-v2-backend/$pkg/" \
        | awk '{gsub(/%/,"",$NF); total+=$NF; n++} END {if (n>0) printf "%.1f", total/n; else print "0.0"}')"
      printf '    %-52s %5s%%\n' "$pkg" "$pct"
    done

echo ""
printf '    %-52s %5s%%\n' "TOTAL" "$TOTAL"
echo ""

# The comparison is done in awk because the shell cannot compare decimals.
if awk -v total="$TOTAL" -v threshold="$THRESHOLD" 'BEGIN { exit !(total < threshold) }'; then
  printf '\033[31mcoverage %s%% is below the %s%% gate\033[0m\n\n' "$TOTAL" "$THRESHOLD" >&2
  echo "    The report is in coverage.filtered.out:" >&2
  echo "    go tool cover -html=coverage.filtered.out" >&2
  echo "" >&2
  exit 1
fi

printf '\033[32mcoverage %s%% meets the %s%% gate\033[0m\n\n' "$TOTAL" "$THRESHOLD"
