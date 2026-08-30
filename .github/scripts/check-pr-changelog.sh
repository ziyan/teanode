#!/usr/bin/env bash
#
# Fail if a change that an operator could notice has no changelog entry.
#
#     .github/scripts/check-pr-changelog.sh <base-ref>
#
# The rule is deliberately loose: only Go and TypeScript under the directories
# that ship count, and touching CHANGELOG.md at all satisfies it. Refactoring,
# documentation, tests and CI need no entry, because an operator cannot see
# them.

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/../.."

readonly BASE="${1:-origin/main}"

changed="$(git diff --name-only "${BASE}"...HEAD)"

if grep -qx 'CHANGELOG.md' <<<"${changed}"; then
  echo "CHANGELOG.md was updated"
  exit 0
fi

# Anything here is code that ends up in the binary an operator runs.
shipping="$(grep -E '^(cmd|internal|web/src)/.*\.(go|ts|tsx)$|^main\.go$' <<<"${changed}" \
  | grep -vE '_test\.go$' || true)"

if [[ -z "${shipping}" ]]; then
  echo "no shipping code changed; no changelog entry needed"
  exit 0
fi

cat >&2 <<EOF
This change touches code that ships but does not update CHANGELOG.md:

$(sed 's/^/  /' <<<"${shipping}")

Add an entry under "## [Unreleased]" describing what an operator would notice.
If they would notice nothing — a refactor, a test, a comment — say so in the
pull request and add the "no changelog" label to skip this check.
EOF
exit 1
