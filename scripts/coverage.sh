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
# The modules run in parallel because what this suite spends its time on is starting
# containers, not running tests: every module costs about the same regardless of how much
# code it has. Sequentially that is the sum of the waits; in parallel it is the longest one.
#
# Parallelism alone was not enough. The number of startups is what dominates, and that was
# cut from about seventy to eighteen by sharing one container per package (D-30). The twelve
# that remain are in pkg/database, whose tests are about the database lifecycle itself.
#
# Each module gets its own profile and its own log; the logs are printed afterwards, in
# module order, because interleaved output from six test binaries is unreadable. Set
# COVERAGE_SEQUENTIAL=1 to get the old behaviour when that interleaving is hiding something.
# How many modules run at once. The bottleneck is container startup rather than CPU, so more
# than the core count still helps -- but only up to the point where the Docker daemon and the
# memory of the machine become the constraint, which on a two-core CI runner arrives early.
# CI sets it explicitly; locally the default is the core count.
JOBS="${COVERAGE_JOBS:-$(getconf _NPROCESSORS_ONLN 2>/dev/null || echo 2)}"

# A test binary that hangs must say so while the job is still alive. This has to stay well
# under the job timeout of the workflow: at the limit, `go test` panics and prints the stack
# of every goroutine, which is the difference between "it hung" and "it hung here".
GO_TEST_TIMEOUT="${COVERAGE_TEST_TIMEOUT:-12m}"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT INT TERM

MODULES=()
while IFS= read -r dir; do
  [ -n "$dir" ] || continue
  [ "$dir" = "$ROOT" ] && continue
  MODULES+=("$dir")
done < <(go list -m -f '{{.Dir}}')

echo "==> ${#MODULES[@]} modules, ${JOBS} at a time, ${GO_TEST_TIMEOUT} per module"
echo ""

# Output is streamed with the module name on every line rather than collected and printed at
# the end. Collecting reads better when everything works and is useless when it does not: a
# run killed by the job timeout prints nothing at all, which is exactly the run whose output
# was needed.
run_module() {
  local dir="$1" profile="$2" rel="$3" status_file="$4" started

  started="$SECONDS"

  ( cd "$dir" && go test \
      ${TAGS:+-tags="$TAGS"} \
      -coverpkg=./... \
      -covermode=atomic \
      -coverprofile="$profile" \
      -timeout="$GO_TEST_TIMEOUT" \
      ./... ) 2>&1 | sed "s|^|[$rel] |"

  local status="${PIPESTATUS[0]}"

  echo "$status" > "$status_file"
  printf '[%s] finished in %ss (exit %s)\n' "$rel" "$((SECONDS - started))" "$status"
}

RUNNING=0

for index in "${!MODULES[@]}"; do
  dir="${MODULES[$index]}"
  rel="${dir#"$ROOT"/}"

  run_module "$dir" "$WORK/profile.$index" "$rel" "$WORK/status.$index" &

  RUNNING=$((RUNNING + 1))

  # A plain `wait -n` would be simpler but needs bash 4.3, and macOS ships 3.2. Waiting for
  # the whole batch is coarser and works everywhere.
  if [ "$RUNNING" -ge "$JOBS" ]; then
    wait
    RUNNING=0
  fi
done

wait

FAILED=""

for index in "${!MODULES[@]}"; do
  rel="${MODULES[$index]#"$ROOT"/}"
  status="$(cat "$WORK/status.$index" 2>/dev/null || echo 1)"

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
