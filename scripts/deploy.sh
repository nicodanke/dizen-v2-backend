#!/usr/bin/env bash
# Triggers a deployment and leaves a record of it (PRD-18 RF-11, D-44).
#
# Two things happen here, and only the first one deploys:
#
#   the webhook   asks Dokploy to pull the images and restart. It is called from here, and
#                 not from a git trigger, because a git event happens BEFORE the images of
#                 that commit exist -- a deployment keyed off it pulls `manifest unknown`.
#                 That race is the reason none of Dokploy's own triggers are used.
#   the tag       `deployed-*` is the record of which commit went out and when. It triggers
#                 nothing; it is there so the history answers "what is running in production
#                 and since when" without opening a dashboard.
#
# The URL is a secret and is never printed. What a leaked one buys is a redeploy of the
# configuration Dokploy already has -- not a way to choose what gets deployed.
#
#   DOKPLOY_WEBHOOK=... deploy.sh <tag> <environment>
set -euo pipefail

tag="${1:?usage: deploy.sh <tag> <environment>}"
environment="${2:?usage: deploy.sh <tag> <environment>}"

: "${DOKPLOY_WEBHOOK:?DOKPLOY_WEBHOOK is empty. Set the repository secret for ${environment}; without it this job would report success and deploy nothing}"

git config user.name "github-actions[bot]"
git config user.email "41898282+github-actions[bot]@users.noreply.github.com"

# Forced, so re-running the pipeline on the same commit moves the ref instead of failing on
# a tag that already exists. Redeploying the same commit is something people legitimately
# want to do, most often during an incident.
git tag -f "$tag" "$GITHUB_SHA"
git push -f origin "refs/tags/${tag}"

echo "==> ${tag} tagged; asking Dokploy to deploy ${environment}"

# --retry covers the transient failure. Without it a blip means the images are published,
# the tag says the commit went out, and nothing was actually deployed -- the worst of the
# three outcomes, because it is the one nobody notices.
status="$(curl -sS -o /dev/null -w '%{http_code}' \
  -X POST \
  --max-time 30 \
  --retry 3 \
  --retry-delay 5 \
  --retry-all-errors \
  "$DOKPLOY_WEBHOOK")"

if [ "$status" -lt 200 ] || [ "$status" -ge 300 ]; then
  echo "::error::Dokploy answered ${status} to the ${environment} deployment request" >&2
  exit 1
fi

echo "==> Dokploy accepted the request (${status})"
