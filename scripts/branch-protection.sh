#!/usr/bin/env bash
# Applies the branch protection of PRD-18 RF-11 and RF-12 to main and develop.
#
# The threshold and the pipeline are only real if the merge button respects them: a required
# check that is not marked as required is a suggestion. This script is what marks them, and
# it is committed rather than done by hand in the settings so the rule is reviewable, and so
# it can be reapplied after somebody changes something in the interface.
#
# It is idempotent: it writes the whole protection every time, so running it twice leaves
# the same state.
#
#   scripts/branch-protection.sh --dry-run     print what it would send
#   scripts/branch-protection.sh               apply it
#
# Requires the GitHub CLI authenticated with admin rights over the repository:
#   brew install gh && gh auth login
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

DRY_RUN=false
[ "${1:-}" = "--dry-run" ] && DRY_RUN=true

# The names are the `name:` of each job in .github/workflows/ci.yml. If a job is renamed
# there, it has to be renamed here too, otherwise the protection waits forever for a check
# that no longer reports.
CHECKS=(
  "static analysis"
  "contract"
  "generated queries"
  "secrets"
  "unit tests"
  "coverage gate"
)

BRANCHES=(main develop)

# Zero by default because the team is one person and GitHub does not let anybody approve
# their own pull request: asking for one approval would make it impossible to merge. The
# pull request is still mandatory, which is what keeps the checks from being skipped.
# Raise it the day there is a second reviewer.
REQUIRED_APPROVALS="${REQUIRED_APPROVALS:-0}"

if ! command -v gh >/dev/null 2>&1; then
  echo "error: the GitHub CLI is not installed" >&2
  echo "       brew install gh && gh auth login" >&2
  exit 1
fi

REPO="$(gh repo view --json nameWithOwner -q .nameWithOwner)"

contexts_json="$(printf '%s\n' "${CHECKS[@]}" | python3 -c 'import json,sys; print(json.dumps([l.rstrip("\n") for l in sys.stdin if l.strip()]))')"

for branch in "${BRANCHES[@]}"; do
  if ! gh api "repos/${REPO}/branches/${branch}" >/dev/null 2>&1; then
    echo "==> ${branch} does not exist in ${REPO}; skipped"
    echo "    create it with: git switch -c ${branch} && git push -u origin ${branch}"
    continue
  fi

  payload="$(cat <<JSON
{
  "required_status_checks": {
    "strict": true,
    "contexts": ${contexts_json}
  },
  "enforce_admins": false,
  "required_pull_request_reviews": {
    "required_approving_review_count": ${REQUIRED_APPROVALS},
    "dismiss_stale_reviews": true,
    "require_code_owner_reviews": false
  },
  "restrictions": null,
  "required_linear_history": true,
  "allow_force_pushes": false,
  "allow_deletions": false,
  "required_conversation_resolution": true
}
JSON
)"

  if [ "$DRY_RUN" = true ]; then
    echo "==> ${REPO}:${branch} (dry run)"
    echo "$payload"
    continue
  fi

  echo "==> protecting ${REPO}:${branch}"
  echo "$payload" | gh api -X PUT "repos/${REPO}/branches/${branch}/protection" \
    -H "Accept: application/vnd.github+json" \
    --input - > /dev/null

  echo "    required checks: ${CHECKS[*]}"
done

echo ""
echo "done. 'strict' is on: a branch has to be up to date with its base before merging,"
echo "which is what makes the green pipeline of the pull request also the pipeline of main."
