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
#
# The modules run in parallel, and the reason is measurable: the suite starts eighteen
# Postgres containers, and every module costs about the same regardless of how much code it
# has, because what is being paid for is container startup and not test logic. Sequentially
# that is the sum of six waits; in parallel it is the longest one. Locally it takes the run
# from about 171 s to about 49 s, and on a CI runner -- where the waits are longer and the
# cores fewer -- the ratio is smaller but the saving is larger.
#
# Each module gets its own profile and its own log; the logs are printed afterwards, in
# module order, because interleaved output from six test binaries is unreadable. Set
# COVERAGE_SEQUENTIAL=1 to get the old behaviour when that interleaving is hiding something.
SEQUENTIAL="${COVERAGE_SEQUENTIAL:-}"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT INT TERM

MODULES=()
while IFS= read -r dir; do
  [ -n "$dir" ] || continue
  [ "$dir" = "$ROOT" ] && continue
  MODULES+=("$dir")
done < <(go list -m -f '{{.Dir}}')

run_module() {
  local dir="$1" profile="$2" log="$3"

  ( cd "$dir" && go test \
      ${TAGS:+-tags="$TAGS"} \
      -coverpkg=./... \
      -covermode=atomic \
      -coverprofile="$profile" \
      -timeout=30m \
      ./... ) > "$log" 2>&1
}

PIDS=()

for index in "${!MODULES[@]}"; do
  dir="${MODULES[$index]}"
  rel="${dir#"$ROOT"/}"

  if [ -n "$SEQUENTIAL" ]; then
    printf '\033[1;34m==> %s\033[0m\n' "$rel"

    # The status is captured from run_module itself. Reading $? after the `cat` below would
    # report whether printing the log worked, which it always does.
    status=0
    run_module "$dir" "$WORK/profile.$index" "$WORK/log.$index" || status=$?
    echo "$status" > "$WORK/status.$index"
    continue
  fi

  run_module "$dir" "$WORK/profile.$index" "$WORK/log.$index" &
  PIDS+=("$!")
done

FAILED=""

for index in "${!MODULES[@]}"; do
  rel="${MODULES[$index]#"$ROOT"/}"

  if [ -n "$SEQUENTIAL" ]; then
    status="$(cat "$WORK/status.$index")"
  else
    status=0
    wait "${PIDS[$index]}" || status=$?
  fi

  printf '\033[1;34m==> %s\033[0m\n' "$rel"
  [ -f "$WORK/log.$index" ] && cat "$WORK/log.$index"

  if [ "$status" != "0" ]; then
    FAILED="${FAILED} ${rel}"
  fi
done

if [ -n "$FAILED" ]; then
  echo "" >&2
  echo "error: the tests failed in:${FAILED}" >&2
  echo "       coverage was not measured" >&2
  exit 1
fi

# Merged in module order, not in the order they happened to finish, so the profile is the
# same on every run and a diff between two runs means a real change.
: > "$PROFILE.tmp"
FIRST=1

for index in "${!MODULES[@]}"; do
  module_profile="$WORK/profile.$index"

  [ -s "$module_profile" ] || continue

  if [ "$FIRST" = "1" ]; then
    cat "$module_profile" >> "$PROFILE.tmp"
    FIRST=0
  else
    # The mode line appears once per profile; only the first is kept.
    tail -n +2 "$module_profile" >> "$PROFILE.tmp"
  fi
done

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
