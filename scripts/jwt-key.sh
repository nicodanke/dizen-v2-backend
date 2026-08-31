#!/usr/bin/env bash
# Generates an Ed25519 key pair and prints it in the shape the destination needs.
#
#   scripts/jwt-key.sh          escaped, one line per key, for a local .env
#   scripts/jwt-key.sh -pem     real PEM, for Doppler or any other secrets manager
#
# The two are not interchangeable: the escaped form only works where something expands the
# escapes, which is godotenv reading a .env file and nowhere else.
#
# The key never touches the repository, in any environment (hard rule 6), and each
# environment gets its own pair.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"

cd "$ROOT/pkg"

go run ./jwt/cmd/keygen "$@"
