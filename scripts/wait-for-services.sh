#!/usr/bin/env bash
# Waits for the five services to answer /livez and /readyz.
#
# This is acceptance criterion 1 of PRD-00, checked by `make up` itself rather than left to
# whoever runs it: a `make up` that returns before the environment is usable is a `make up`
# that lies.
set -euo pipefail

# service:port on the host, as published by the compose file.
SERVICES="identity:8081 tours:8082 booking:8083 admin:8084 mail-dispatcher:8085"

# RNF-1 gives the whole environment three minutes from cold.
DEADLINE=$(( $(date +%s) + 180 ))

printf '==> waiting for the services\n'

for entry in $SERVICES; do
  name="${entry%%:*}"
  port="${entry##*:}"

  printf '    %-16s ' "$name"

  while true; do
    # `|| live=""` is load bearing twice over. Under `set -e` a failing curl would abort
    # this script with curl's own exit code -- which is exactly what happens while a
    # service is starting: the port is published, so the connection is accepted and closed
    # before the app listens, and curl exits 52 or 56. A wait loop that dies on the
    # condition it is waiting for is worse than no wait loop. Assigning rather than
    # echoing also keeps curl's own "000" from concatenating with a fallback.
    live=$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 "http://localhost:$port/livez" 2>/dev/null) || live=""
    ready=$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 "http://localhost:$port/readyz" 2>/dev/null) || ready=""
    live="${live:-000}"
    ready="${ready:-000}"

    if [ "$live" = "200" ] && [ "$ready" = "200" ]; then
      printf 'livez 200  readyz 200\n'
      break
    fi

    if [ "$(date +%s)" -ge "$DEADLINE" ]; then
      printf 'TIMEOUT (livez %s, readyz %s)\n' "$live" "$ready" >&2
      echo "" >&2
      echo "    /readyz reports which dependency is failing:" >&2
      curl -s --max-time 5 "http://localhost:$port/readyz" >&2 || echo "    (the service is not answering at all)" >&2
      echo "" >&2
      exit 1
    fi

    sleep 2
  done
done

cat <<'BANNER'

==> the environment is up

    Traefik dashboard   http://localhost:8090
    RabbitMQ panel      http://localhost:15672   (dizen / dizen)
    MinIO console       http://localhost:9001    (dizen / dizen12345)
    Prometheus          http://localhost:9090
    Grafana             http://localhost:3000    (dizen / dizen)
    Jaeger              http://localhost:16686

    identity            http://localhost:8081    gRPC on 9091
    tours               http://localhost:8082    gRPC on 9092
    booking             http://localhost:8083    gRPC on 9093
    admin               http://localhost:8084    gRPC on 9094
    mail-dispatcher     http://localhost:8085    no public API

BANNER
