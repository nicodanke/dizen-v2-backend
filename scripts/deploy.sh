#!/usr/bin/env bash
# Triggers a deployment and leaves a record of it (PRD-18 RF-11, D-44).
#
# Two things happen here, and only the first one deploys:
#
#   the API call  asks Dokploy to deploy one compose, by id. It is made from here, and not
#                 by any of Dokploy's own triggers, because all of those react to a git
#                 event -- which happens BEFORE the images of that commit exist, so the
#                 deployment pulls `manifest unknown`. This runs after they are published.
#   the tag       `deployed-*` triggers nothing. It is the record of which commit went out
#                 and when, so the history answers "what is running, and since when".
#
# The API is used rather than the webhook Dokploy offers for git providers. That webhook is
# governed by the project's own Autodeploy and Trigger settings, which is exactly the
# machinery being avoided, and it answers 400 to anything that is not a provider payload.
#
#   DOKPLOY_URL=... DOKPLOY_API_KEY=... DOKPLOY_COMPOSE_ID=... deploy.sh <tag> <environment>
set -euo pipefail

tag="${1:?usage: deploy.sh <tag> <environment>}"
environment="${2:?usage: deploy.sh <tag> <environment>}"

: "${DOKPLOY_URL:?DOKPLOY_URL is empty. Without it this job would report success and deploy nothing}"
: "${DOKPLOY_API_KEY:?DOKPLOY_API_KEY is empty. Without it this job would report success and deploy nothing}"
: "${DOKPLOY_COMPOSE_ID:?DOKPLOY_COMPOSE_ID is empty: the id of the ${environment} compose in Dokploy}"

git config user.name "github-actions[bot]"
git config user.email "41898282+github-actions[bot]@users.noreply.github.com"

# Forced, so re-running the pipeline on the same commit moves the ref instead of failing on
# a tag that already exists. Redeploying the same commit is something people legitimately
# want to do, most often during an incident.
git tag -f "$tag" "$GITHUB_SHA"
git push -f origin "refs/tags/${tag}"

subject="$(git log -1 --pretty=%s "$GITHUB_SHA")"

# freshVolumes is false, and never anything else. The API's own example shows `true`, which
# recreates the volumes from scratch: on production that is deleting data to deploy a commit.
body="$(cat <<JSON
{
  "composeId": "${DOKPLOY_COMPOSE_ID}",
  "title": "${tag}",
  "description": "${subject//\"/\'}",
  "freshVolumes": false
}
JSON
)"

echo "==> ${tag} tagged; asking Dokploy to deploy ${environment}"

# Dokploy's OpenAPI page lists this as /compose.deploy and does not say whether the server
# base includes /api. It does. Verified against the running instance rather than assumed,
# and pinned here: a fallback that tries the other path would turn a 404 -- the endpoint
# moved, the API changed -- into the error message of a second call that was never going to
# work either, which is the wrong thing to be told when it breaks.
endpoint="${DOKPLOY_URL%/}/api/compose.deploy"

status="$(curl -sS -o /tmp/dokploy.out -w '%{http_code}' \
  -X POST \
  -H "x-api-key: ${DOKPLOY_API_KEY}" \
  -H 'Content-Type: application/json' \
  -d "$body" \
  --max-time 60 \
  --retry 3 \
  --retry-delay 5 \
  --retry-all-errors \
  "$endpoint")"

if [ "$status" -lt 200 ] || [ "$status" -ge 300 ]; then
  echo "::error::Dokploy answered ${status} deploying ${environment}" >&2
  echo "endpoint: ${endpoint}" >&2
  head -c 500 /tmp/dokploy.out >&2 || true
  exit 1
fi

echo "==> Dokploy accepted the request (${status})"
