#!/usr/bin/env bash
# Generates the OpenAPI v3 contract, one file per service, into gen/openapi/.
#
# gnostic (protoc-gen-openapi) only offers two modes: one file per .proto -- which leaves
# empty files for packages without RPCs, such as common/v1 -- or a single file for the
# whole API. Neither works: dizen-v2-web generates its client per service. The way around
# it is to invoke buf once per service package, narrowing the input with --path.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT_DIR="$ROOT/gen/openapi"
BUF="$ROOT/bin/buf"

# buf looks for local plugins on the PATH.
PATH="$ROOT/bin:$PATH"
export PATH

rm -rf "$OUT_DIR"
mkdir -p "$OUT_DIR"

cd "$ROOT/proto"

for pkg_dir in dizen/*/v1; do
  [ -d "$pkg_dir" ] || continue

  service_name="$(basename "$(dirname "$pkg_dir")")"

  # Packages without any `service` produce no REST routes; skip them quietly.
  if ! grep -rlq '^service ' "$pkg_dir" 2>/dev/null; then
    echo "    (no RPCs) $service_name"
    continue
  fi

  "$BUF" generate --template buf.gen.openapi.yaml --path "$pkg_dir"

  if [ ! -f "$OUT_DIR/openapi.yaml" ]; then
    echo "error: gnostic produced no openapi.yaml for $service_name" >&2
    exit 1
  fi

  # gnostic always writes "openapi.yaml"; rename it to the matching service.
  mv "$OUT_DIR/openapi.yaml" "$OUT_DIR/$service_name.yaml"
  echo "    gen/openapi/$service_name.yaml"
done
