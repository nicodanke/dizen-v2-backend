#!/usr/bin/env bash
# Generates an Ed25519 key pair for development and prints it in the shape the .env expects.
#
# Development only. In staging and production the keys are managed by Dokploy as secrets and
# never touch the repository (hard rule 6).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"

cd "$ROOT/pkg"

go run ./jwt/cmd/keygen
