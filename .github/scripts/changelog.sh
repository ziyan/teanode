#!/usr/bin/env bash
#
# Print the changelog entry for one version, for use as release notes.
#
#     .github/scripts/changelog.sh 0.2.0
#
# Reads CHANGELOG.md and prints the section between that version's heading and
# the next one. Exits non-zero if there is no such section, so a release cannot
# be published with empty notes.

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/../.."

if [[ $# -ne 1 ]]; then
  echo "usage: $0 <version>" >&2
  exit 1
fi

readonly VERSION="${1#v}"

section="$(awk -v version="${VERSION}" '
  # Headings look like "## [0.2.0] - 2026-08-18" or "## [Unreleased]".
  /^## / {
    if (printing) { exit }
    if (index($0, "[" version "]") > 0) { printing = 1; next }
  }
  printing { print }
' CHANGELOG.md)"

# Trim leading and trailing blank lines.
section="$(printf '%s' "${section}" | sed -e '/./,$!d' -e ':a' -e '/^\n*$/{$d;N;ba' -e '}')"

if [[ -z "${section}" ]]; then
  echo "no changelog section for ${VERSION}; add one to CHANGELOG.md" >&2
  exit 1
fi

printf '%s\n' "${section}"
