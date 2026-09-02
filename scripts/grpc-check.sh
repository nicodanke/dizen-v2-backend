#!/usr/bin/env bash
# Verifies the gRPC surface of a deployed environment (PRD-18 RF-6, acceptance criterion 3).
#
# This is the check the PRD singles out, and for a reason: TLS terminating at Traefik while
# HTTP/2 continues in the clear to the service is the part of this deployment that most often
# breaks, and it breaks in a way that looks like anything but itself -- the client reports an
# unreadable framing error rather than a routing or a certificate problem.
#
# Reflection is deliberately not used. The gRPC host routes by proto package (D-24), and a
# reflection call travels under /grpc.reflection..., which matches no router: Traefik answers
# a plain 404 and the client tries to parse it as a gRPC frame. So the schema is supplied
# instead, built by buf so that the imports from buf.build resolve -- which is why passing
# the .proto file directly does not work.
#
#   make grpc-check HOST=grpc.staging.v2.dizen.pro:443
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

HOST="${1:-}"

if [ -z "$HOST" ]; then
  echo "usage: make grpc-check HOST=grpc.staging.v2.dizen.pro:443" >&2
  exit 2
fi

if ! command -v grpcurl >/dev/null 2>&1; then
  echo "error: grpcurl is not installed" >&2
  echo "       brew install grpcurl" >&2
  exit 1
fi

DESCRIPTOR="$(mktemp -t dizen-descriptor).binpb"
trap 'rm -f "$DESCRIPTOR"' EXIT

echo "==> building the descriptor from proto/"
( cd proto && "$ROOT/bin/buf" build -o "$DESCRIPTOR" )

echo "==> $HOST"
echo ""

# HealthPing is the reference RPC of PRD-00 and one of the few methods that is public, so it
# answers without a token: what is being verified here is the transport, not authorisation.
grpcurl -protoset "$DESCRIPTOR" -d '{}' "$HOST" \
  dizen.identity.v1.HealthService/HealthPing

echo ""
echo "==> gRPC answers over TLS with HTTP/2 end to end"
