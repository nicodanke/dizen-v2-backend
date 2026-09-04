#!/usr/bin/env bash
# Registers the release of each service in Sentry (PRD-24 RF-8).
#
# The binary already reports which release it is: pkg/observability/sentry builds the same
# string this does, from the same version and commit. What it cannot do is tell Sentry which
# commits went into it -- so a regression can be seen, but not attributed. That is what this
# adds, and it has to run from CI because only CI knows the git history.
#
# One release per service, because there is one project per service.
#
#   SENTRY_AUTH_TOKEN=... SENTRY_ORG=... sentry-release.sh <version> <commit> <environment>
#
# A missing token is not an error: it skips. An environment without Sentry projects should
# still deploy, the same way an empty DSN disables reporting rather than failing.
set -euo pipefail

version="${1:?usage: sentry-release.sh <version> <commit> <environment>}"
commit="${2:?usage: sentry-release.sh <version> <commit> <environment>}"
environment="${3:?usage: sentry-release.sh <version> <commit> <environment>}"

if [ -z "${SENTRY_AUTH_TOKEN:-}" ] || [ -z "${SENTRY_ORG:-}" ]; then
  echo "==> SENTRY_AUTH_TOKEN or SENTRY_ORG is not set; skipping the release registration"
  exit 0
fi

SERVICES="identity tours booking admin mail-dispatcher"

for service in $SERVICES; do
  # The project slug defaults to the service name and can be overridden per service, since
  # Sentry slugs are chosen when the project is created and need not match.
  var="SENTRY_PROJECT_$(echo "$service" | tr '[:lower:]-' '[:upper:]_')"
  project="${!var:-$service}"

  # Exactly what the binary reports. If the two ever disagree, Sentry shows a release with
  # no events beside events with no release, which looks like a reporting failure and is
  # not one -- so this string is the contract between the two.
  release="${service}@${version}@${commit}"

  echo "==> ${project}: ${release}"

  sentry-cli releases new "$release" --project "$project"

  # --local reads the git history that is already checked out, instead of asking Sentry to
  # fetch it. It needs no GitHub integration on the Sentry side, which is one fewer thing
  # to keep connected; switch to --auto once that integration exists and is worth it.
  sentry-cli releases set-commits "$release" --local --ignore-missing

  sentry-cli releases finalize "$release"
  sentry-cli releases deploys "$release" new --env "$environment"
done

echo "==> ${environment}: registered the release of $(echo "$SERVICES" | wc -w | tr -d ' ') services"
