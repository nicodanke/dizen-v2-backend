#!/usr/bin/env bash
# Validates the versioned Yaak collection (RF-17c, 03 section 8.2 rule 5).
#
# The check itself is in Go (tools/apicheck) because Go is the one toolchain this repository
# already requires: it has to run everywhere without anybody installing a YAML parser.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

go run ./tools/apicheck api-client
