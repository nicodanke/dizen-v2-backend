#!/usr/bin/env bash
# Checks that everything needed to work on this repository is installed and running.
#
# It is meant to be the first command somebody runs on a new machine, and the first one to
# run when something stops working: it turns "it does not build" into a specific missing
# thing plus the command that installs it.
#
# Exit status is 0 only if every required check passed. Optional checks warn and do not fail.
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

# shellcheck disable=SC2034
FAILURES=0
WARNINGS=0

BOLD=$'\033[1m'; RESET=$'\033[0m'
GREEN=$'\033[32m'; RED=$'\033[31m'; YELLOW=$'\033[33m'; DIM=$'\033[2m'

# ok/warn/fail print one aligned result line.
ok()   { printf '  %s%-22s%s %sok%s      %s\n'   "$BOLD" "$1" "$RESET" "$GREEN" "$RESET" "${2:-}"; }
warn() { printf '  %s%-22s%s %swarn%s    %s\n'   "$BOLD" "$1" "$RESET" "$YELLOW" "$RESET" "${2:-}"; WARNINGS=$((WARNINGS+1)); }
fail() { printf '  %s%-22s%s %sMISSING%s %s\n'   "$BOLD" "$1" "$RESET" "$RED" "$RESET" "${2:-}"; FAILURES=$((FAILURES+1)); }
hint() { printf '  %s%-22s%s         %s%s%s\n'   "$BOLD" "" "$RESET" "$DIM" "$1" "$RESET"; }
section() { printf '\n%s%s%s\n' "$BOLD" "$1" "$RESET"; }

# --- required toolchain -----------------------------------------------------
section "Toolchain"

if command -v go >/dev/null 2>&1; then
  GO_VERSION="$(go env GOVERSION 2>/dev/null | sed 's/^go//')"
  GO_MAJOR="${GO_VERSION%%.*}"
  GO_REST="${GO_VERSION#*.}"
  GO_MINOR="${GO_REST%%.*}"

  # The go directive in every go.mod is 1.27; below that the workspace will not build.
  if [ "$GO_MAJOR" -gt 1 ] || { [ "$GO_MAJOR" -eq 1 ] && [ "$GO_MINOR" -ge 27 ]; }; then
    ok "go" "$GO_VERSION"
  else
    # GOTOOLCHAIN=auto downloads the right toolchain on demand, so an older go is usually
    # fine; it is only a problem when the download is disabled.
    if [ "$(go env GOTOOLCHAIN)" = "local" ]; then
      fail "go" "$GO_VERSION, and GOTOOLCHAIN=local prevents upgrading"
      hint "go env -w GOTOOLCHAIN=auto   (or install Go 1.27+)"
    else
      warn "go" "$GO_VERSION; the 1.27 toolchain will be downloaded on first build"
    fi
  fi
else
  fail "go" "not installed"
  hint "brew install go"
fi

if command -v git >/dev/null 2>&1; then
  ok "git" "$(git --version | awk '{print $3}')"
else
  fail "git" "not installed"
  hint "xcode-select --install"
fi

if command -v curl >/dev/null 2>&1; then
  ok "curl" "$(curl --version | head -1 | awk '{print $2}')"
else
  fail "curl" "not installed"
fi

# --- docker -----------------------------------------------------------------
section "Docker"

if command -v docker >/dev/null 2>&1; then
  ok "docker" "$(docker --version | awk '{print $3}' | tr -d ,)"

  if docker info >/dev/null 2>&1; then
    ok "docker daemon" "running"
  else
    fail "docker daemon" "installed but not running"
    hint "open -a Docker   (or start Docker Desktop / colima)"
  fi

  if docker compose version >/dev/null 2>&1; then
    ok "docker compose" "$(docker compose version --short 2>/dev/null)"
  else
    fail "docker compose" "the compose plugin is not available"
    hint "update Docker Desktop, or install docker-compose-plugin"
  fi
else
  fail "docker" "not installed"
  hint "brew install --cask docker"
fi

# --- optional ---------------------------------------------------------------
section "Optional"

if command -v dart >/dev/null 2>&1; then
  ok "dart" "$(dart --version 2>&1 | awk '{print $4}')"
else
  # Only make proto needs it, and only to regenerate the Dart package. Everything else
  # works without it because the generated code is committed.
  warn "dart" "not installed; only needed by 'make proto'"
  hint "brew install dart-sdk"
fi

# --- pinned tools -----------------------------------------------------------
section "Pinned tools (./bin)"

# The expected version of each tool comes from tools/versions.mk, which is the single
# source of truth.
read_pin() {
  grep -E "^$1[[:space:]]*:=" tools/versions.mk 2>/dev/null | sed 's/.*:=[[:space:]]*//' | tr -d ' '
}

check_tool() {
  local name="$1" pin_var="$2" version_cmd="$3"
  local binary="$ROOT/bin/$name"
  local expected actual

  expected="$(read_pin "$pin_var")"

  if [ ! -x "$binary" ]; then
    fail "$name" "not installed in ./bin"
    return
  fi

  # stderr is folded in: golang-migrate prints its version there.
  actual="$(cd "$ROOT" && eval "$version_cmd" 2>&1 | head -1 | tr -d ' \n')"

  # golang-migrate installed with `go install` reports "dev", because its real version is
  # injected with ldflags at release time. There is nothing to compare, so presence is all
  # that can be checked.
  if [ -z "$actual" ] || [ "$actual" = "dev" ]; then
    ok "$name" "installed (does not report a version)"
    return
  fi

  # buf and sqlc print the version without a leading v; normalize both sides.
  if [ "${actual#v}" = "${expected#v}" ]; then
    ok "$name" "$actual"
  else
    warn "$name" "$actual installed, tools/versions.mk pins $expected"
  fi
}

if [ ! -d "$ROOT/bin" ]; then
  fail "./bin" "the tools are not installed"
  hint "make tools"
else
  check_tool buf           BUF_VERSION           './bin/buf --version'
  check_tool sqlc          SQLC_VERSION          './bin/sqlc version'
  check_tool migrate       MIGRATE_VERSION       './bin/migrate -version'
  check_tool golangci-lint GOLANGCI_LINT_VERSION './bin/golangci-lint --version | awk "{print \$4}"'

  for tool in protoc-gen-go protoc-gen-go-grpc protoc-gen-grpc-gateway protoc-gen-openapi mockery; do
    if [ -x "$ROOT/bin/$tool" ]; then
      ok "$tool" "installed"
    else
      fail "$tool" "not installed in ./bin"
    fi
  done

  if [ -e "$ROOT/bin/protoc-gen-dart" ]; then
    ok "protoc-gen-dart" "linked"
  else
    warn "protoc-gen-dart" "not linked; only needed by 'make proto'"
    hint "make tools-dart"
  fi
fi

if [ "${FAILURES}" -gt 0 ] && [ ! -d "$ROOT/bin" ]; then
  : # the hint above already says what to do
fi

# --- ports ------------------------------------------------------------------
section "Ports"

# Every port the compose file publishes, with what uses it.
PORTS="80:traefik 3000:grafana 4317:jaeger-otlp 5432:identity-db 5433:tours-db \
5434:booking-db 5435:admin-db 5436:mail-db 5672:rabbitmq 6379:redis \
8081:identity 8082:tours 8083:booking 8084:admin 8085:mail-dispatcher \
8090:traefik-dashboard 9000:minio 9001:minio-console 9090:prometheus \
9091:identity-grpc 9092:tours-grpc 9093:booking-grpc 9094:admin-grpc \
15672:rabbitmq-panel 16686:jaeger-ui"

CONFLICTS=""

for entry in $PORTS; do
  port="${entry%%:*}"
  owner="${entry##*:}"

  if ! command -v lsof >/dev/null 2>&1; then
    break
  fi

  pid="$(lsof -nP -iTCP:"$port" -sTCP:LISTEN -t 2>/dev/null | head -1)"

  [ -z "$pid" ] && continue

  # A port held by Docker is our own environment, not a conflict.
  process="$(ps -p "$pid" -o comm= 2>/dev/null || echo unknown)"

  case "$process" in
    *docker*|*Docker*|*com.docke*) continue ;;
  esac

  CONFLICTS="$CONFLICTS $port($owner, held by ${process##*/})"
done

if [ -z "$CONFLICTS" ]; then
  ok "ports" "no conflicts on the 24 published ports"
else
  warn "ports" "in use by something other than Docker:"
  for conflict in $CONFLICTS; do
    hint "$conflict"
  done
  hint "stop that process, or change the mapping in deploy/docker-compose.yml"
fi

# --- environment ------------------------------------------------------------
section "Environment"

if docker info >/dev/null 2>&1; then
  RUNNING="$(docker compose -f deploy/docker-compose.yml ps --services --status running 2>/dev/null | wc -l | tr -d ' ')"

  if [ "$RUNNING" = "0" ]; then
    warn "compose" "not running"
    hint "make up"
  else
    ok "compose" "$RUNNING containers running"

    for entry in identity:8081 tours:8082 booking:8083 admin:8084 mail-dispatcher:8085; do
      name="${entry%%:*}"
      port="${entry##*:}"

      # curl already prints 000 when it cannot connect, so the fallback only covers curl
      # itself being absent; without the guard the two concatenate into "000000".
      code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 3 "http://localhost:$port/readyz" 2>/dev/null)"
      code="${code:-000}"

      if [ "$code" = "200" ]; then
        ok "$name" "readyz 200"
      elif [ "$code" = "503" ]; then
        warn "$name" "readyz 503, a dependency is down"
        hint "curl -s localhost:$port/readyz   # says which one"
      else
        warn "$name" "not answering (http $code)"
        hint "make logs SERVICE=$name"
      fi
    done
  fi
else
  warn "compose" "cannot be checked, the Docker daemon is not running"
fi

# --- summary ----------------------------------------------------------------
printf '\n'

if [ "$FAILURES" -gt 0 ]; then
  printf '%s%d required check(s) failed%s, %d warning(s).\n\n' "$RED" "$FAILURES" "$RESET" "$WARNINGS"
  exit 1
fi

if [ "$WARNINGS" -gt 0 ]; then
  printf '%sEverything required is in place%s, with %d warning(s).\n\n' "$GREEN" "$RESET" "$WARNINGS"
  exit 0
fi

printf '%sEverything is in place.%s\n\n' "$GREEN" "$RESET"
