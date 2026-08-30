#!/usr/bin/env bash
# Builds the module it is invoked in. `make build` calls it through
# scripts/for-each-module.sh, once per module of the workspace.
#
# Binaries go to dist/<module>/ instead of being dropped next to the sources: all five
# services keep their main package in cmd/server, so without separating them per module
# they would overwrite each other. Modules without a main package are only compiled.
set -euo pipefail

ROOT="${ROOT_DIR:?ROOT_DIR is required}"
REL="${MODULE_REL:?MODULE_REL is required}"

MAINS="$(go list -f '{{if eq .Name "main"}}{{.ImportPath}}{{end}}' ./...)"

if [ -z "$MAINS" ]; then
  go build ./...
  exit 0
fi

mkdir -p "$ROOT/dist/$REL"
go build -o "$ROOT/dist/$REL/" ./...
