#!/usr/bin/env bash
# Fails when the run's commit is no longer the tip of the branch it is on.
#
# Concurrency supersedes a run while it is in flight, which covers a merge landing while an
# older run waits for its approval. It does nothing about re-running an old run from the
# GitHub UI, and that path is the damaging one: the re-run's `images` job republishes the
# moving tag -- `:staging`, `:production` -- pointing at the old commit, so the rewind
# outlives the run. Every later deployment, including one triggered by hand from Dokploy,
# then picks up the old image.
#
# So this runs BEFORE the images are built, not only before they are deployed.
#
# A tag is exempt: it names one commit on purpose.
#
# This is not the rollback path and does not block one. A rollback points IMAGE_TAG at a
# `sha-` or version tag in Dokploy and needs no pipeline at all.
set -euo pipefail

if [ "${GITHUB_REF_TYPE:-}" != "branch" ]; then
  echo "==> ${GITHUB_REF_NAME:-unknown} is not a branch; nothing to supersede it"
  exit 0
fi

git fetch --no-tags --quiet origin "$GITHUB_REF_NAME"
tip="$(git rev-parse FETCH_HEAD)"

if [ "$tip" != "$GITHUB_SHA" ]; then
  echo "::error::${GITHUB_REF_NAME} is at ${tip:0:7}, this run is for ${GITHUB_SHA:0:7}" >&2
  echo "This run is superseded. Building from it would rewind the moving image tag to an" >&2
  echo "older build, for every later deployment and not just this one. To roll back, point" >&2
  echo "IMAGE_TAG at a sha- tag in Dokploy instead." >&2
  exit 1
fi

echo "==> ${GITHUB_SHA:0:7} is the tip of ${GITHUB_REF_NAME}"
