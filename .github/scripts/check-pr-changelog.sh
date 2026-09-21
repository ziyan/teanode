#!/usr/bin/env bash
#
# Fail if a change that an operator could notice has no changelog entry.
#
#     .github/scripts/check-pr-changelog.sh <base-ref> [description-file]
#
# The entry can be in either of the two places a release reads: the Changelog
# block of the pull request description, or the Unreleased section of
# CHANGELOG.md. The first is where most of them belong — it is reviewed
# alongside the change it describes — and the second is for somebody
# committing to main.
#
# The rule is deliberately loose about what needs one: only Go and TypeScript
# under the directories that ship count. Refactoring, documentation, tests and
# CI need no entry, because an operator cannot see them.

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/../.."

readonly BASE="${1:-origin/main}"
readonly DESCRIPTION="${2:-}"
readonly PARSER=".github/scripts/pull-request-entries.awk"

changed="$(git diff --name-only "${BASE}"...HEAD)"

# Anything here is code that ends up in the binary an operator runs. CSS
# counts: a stylesheet is where the dashboard says what fits on a phone, and a
# change nobody can see is not the same as a change with no code in it.
shipping="$(grep -E '^(cmd|internal|web/src)/.*\.(go|ts|tsx|css)$|^main\.go$' <<<"${changed}" \
  | grep -vE '_test\.go$' || true)"

if [[ -z "${shipping}" ]]; then
  echo "No shipping code changed; no changelog entry needed."
  exit 0
fi

if grep -qx 'CHANGELOG.md' <<<"${changed}"; then
  echo "CHANGELOG.md was updated."
  exit 0
fi

if [[ -n "${DESCRIPTION}" && -f "${DESCRIPTION}" ]]; then
  entries="$(mktemp -d)"
  trap 'rm -rf "${entries}"' EXIT
  awk -v number=0 -v outdir="${entries}" -f "${PARSER}" <"${DESCRIPTION}"
  if [[ -n "$(ls -A "${entries}")" ]]; then
    echo "The description has a changelog entry."
    exit 0
  fi
fi

cat >&2 <<EOF
This change touches code that ships and has no changelog entry:

$(sed 's/^/  /' <<<"${shipping}")

Fill in the Changelog block of the description — that block becomes the release
notes — or add an entry under "## [Unreleased]" in CHANGELOG.md.

If an operator would notice nothing — a refactor, a test, a comment — say so in
the description and add the "no changelog" label to skip this check.
EOF
exit 1
