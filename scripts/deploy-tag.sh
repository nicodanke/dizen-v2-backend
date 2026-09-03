#!/usr/bin/env bash
# Moves the refs a deployment keys off (PRD-18 RF-11, D-36, D-43).
#
# Two refs, with two different jobs:
#
#   the branch  deploy-<environment>   is what Dokploy watches, and it is a BRANCH because
#                                      Dokploy's tag trigger does not filter by name: with
#                                      both projects on "on tag", every tag deployed both
#                                      environments. Its branch trigger does filter, and
#                                      always did -- that was how staging deployed before
#                                      the images moved to CI.
#   the tag     deployed-*             is the record of which commit went out and when,
#                                      which a moving branch does not preserve.
#
# Either way the push has to happen AFTER the images are published. A git event -- a branch
# push, or the `v*` tag a person pushes -- happens before they exist, so a deployment keyed
# off it pulls `manifest unknown`. That race is the reason branch auto-deploy was turned off
# in the first place, and pushing the branch from here is what removes it: this runs when the
# images are already in the registry.
#
#   deploy-tag.sh <tag> <environment>
set -euo pipefail

tag="${1:?usage: deploy-tag.sh <tag> <environment>}"
environment="${2:?usage: deploy-tag.sh <tag> <environment>}"

branch="deploy-${environment}"

git config user.name "github-actions[bot]"
git config user.email "41898282+github-actions[bot]@users.noreply.github.com"

# Both forced. Re-running the pipeline on the same commit has to move the refs and trigger a
# fresh deployment rather than fail on something that already exists -- redeploying the same
# version is something people legitimately want to do, most often during an incident. The
# branch is force-pushed for the same reason, and because a rollback moves it backwards.
git tag -f "$tag" "$GITHUB_SHA"
git push -f origin "refs/tags/${tag}"

git push -f origin "${GITHUB_SHA}:refs/heads/${branch}"

echo "==> ${tag} tagged, ${branch} moved to ${GITHUB_SHA:0:7}"
echo "==> Dokploy deploys ${environment} from ${branch}"
