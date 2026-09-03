#!/usr/bin/env bash
# Moves the ref Dokploy deploys from (PRD-18 RF-11, D-36).
#
# Dokploy watches the repository for tags rather than listening on a webhook: a ref carries
# no secret, cannot be triggered by whoever finds a URL, and leaves in the history a record
# of which commit went out and when.
#
# The tag has to be pushed by something that runs AFTER the images are published. A git
# event -- a branch push, or the `v*` tag a person pushes to cut a release -- happens before
# they exist, so a deployment keyed off it pulls `manifest unknown`. That is a race, not a
# configuration problem, and it is the whole reason this exists.
#
#   deploy-tag.sh <tag> <environment>
set -euo pipefail

tag="${1:?usage: deploy-tag.sh <tag> <environment>}"
environment="${2:?usage: deploy-tag.sh <tag> <environment>}"

git config user.name "github-actions[bot]"
git config user.email "41898282+github-actions[bot]@users.noreply.github.com"

# Forced, so re-running the pipeline on the same commit moves the ref and triggers a fresh
# deployment instead of failing on a tag that already exists. Redeploying the same version is
# something people legitimately want to do, most often during an incident.
git tag -f "$tag" "$GITHUB_SHA"
git push -f origin "refs/tags/${tag}"

echo "==> ${tag} pushed; Dokploy deploys ${environment} from it"
