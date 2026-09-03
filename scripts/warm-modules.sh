#!/usr/bin/env bash
# Downloads the modules of every workspace module, retrying transient failures.
#
# `proxy.golang.org` fails a download every so often -- an HTTP/2 stream error mid-zip is
# the usual shape -- and Go does not retry it. Left alone, one blip anywhere in about a
# hundred modules fails whichever job hit it, which costs the whole pipeline for a reason
# that has nothing to do with the code.
#
# Go's own proxy fallback does not cover this: it moves to the next GOPROXY entry on a 404,
# not on a broken transfer. So the retry has to be here.
#
# This is a warm-up, not a gate. It runs after the module cache is restored, so on a cache
# hit there is nothing to fetch and it costs a few seconds of verification.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

attempts=3

download() {
  local failed=0

  while IFS= read -r dir; do
    [ -n "$dir" ] || continue

    if ! ( cd "$dir" && go mod download ); then
      failed=1
    fi
  done < <(go list -m -f '{{.Dir}}')

  return "$failed"
}

for attempt in $(seq 1 "$attempts"); do
  if download; then
    echo "==> modules ready"
    exit 0
  fi

  if [ "$attempt" -lt "$attempts" ]; then
    delay=$(( attempt * 10 ))
    echo "==> module download failed (attempt ${attempt}/${attempts}); retrying in ${delay}s" >&2
    sleep "$delay"
  fi
done

echo "::error::could not download the modules after ${attempts} attempts" >&2
exit 1
