#!/usr/bin/env bash
#
# Turn whatever has piled up under "Unreleased" into a release: work out the
# next version, date the section, and leave the file ready for the next change.
#
#     .github/scripts/cut-release.sh              # say what would happen
#     .github/scripts/cut-release.sh --write      # rewrite CHANGELOG.md
#     .github/scripts/cut-release.sh --write --major
#
# It does not touch git. Deciding what the version is and writing it down is
# one thing; committing, tagging and pushing is another, and the workflow does
# that with the credentials for it. Keeping them apart means this can be run on
# a laptop to see what would happen, which is the first thing anybody wants
# from a release tool.
#
# The version comes from the entries:
#
#   - "### Added" or "### Removed" means a new minor version
#   - anything else means a new patch version
#   - --major means a new major version, and is never inferred: deciding that a
#     change breaks whoever is running this is a judgement a person makes
#   - no tags at all means this is the first release, 0.1.0
#
# Nothing under Unreleased is not a failure. Most pushes are a refactor, a
# test or a comment, and for those the answer is that there is nothing to
# release, which it says and then exits 0.

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/../.."

readonly CHANGELOG="CHANGELOG.md"

write=0
major=0
for argument in "$@"; do
  case "${argument}" in
    --write) write=1 ;;
    --major) major=1 ;;
    *) echo "unknown argument: ${argument}" >&2; exit 1 ;;
  esac
done

# report <released> [version]
#
# Says what happened, on stdout for a person and in GITHUB_OUTPUT for the
# workflow. Both, always: a step that only speaks to the machine is a step
# nobody can debug from the log.
report() {
  local released="$1" version="${2:-}"
  if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
    {
      echo "released=${released}"
      echo "version=${version}"
      echo "tag=${version:+v${version}}"
    } >>"${GITHUB_OUTPUT}"
  fi
}

unreleased="$(awk '
  /^## / {
    if (printing) { exit }
    if (index($0, "[Unreleased]") > 0) { printing = 1; next }
  }
  printing { print }
' "${CHANGELOG}")"

if [[ -z "$(tr -d '[:space:]' <<<"${unreleased}")" ]]; then
  echo "Nothing under [Unreleased], so there is nothing to release."
  report false
  exit 0
fi

# The highest version tag, not the most recent one: a tag pushed out of order
# must not make the next release go backwards.
latest="$(git tag --list 'v*' | sed 's/^v//' | sort -V | tail -1)"

if [[ -z "${latest}" ]]; then
  version="0.1.0"
  reason="the first release"
else
  IFS=. read -r current_major current_minor current_patch <<<"${latest}"
  if (( major )); then
    version="$((current_major + 1)).0.0"
    reason="asked for by hand"
  elif grep -qE '^### (Added|Removed)' <<<"${unreleased}"; then
    version="${current_major}.$((current_minor + 1)).0"
    reason="something was added or removed"
  else
    version="${current_major}.${current_minor}.$((current_patch + 1))"
    reason="fixes and changes only"
  fi
fi

echo "Next version: ${version} (${reason}; the last tag was ${latest:-none})."

if (( ! write )); then
  echo "Nothing was written. Pass --write to date the changelog."
  report true "${version}"
  exit 0
fi

# The heading goes in below [Unreleased] and above the entries, which leaves
# [Unreleased] empty and ready for the next change.
today="$(date -u +%Y-%m-%d)"
awk -v heading="## [${version}] - ${today}" '
  !done && /^## \[Unreleased\]/ {
    print
    print ""
    print heading
    done = 1
    next
  }
  { print }
' "${CHANGELOG}" >"${CHANGELOG}.next"
mv "${CHANGELOG}.next" "${CHANGELOG}"

echo "Wrote ${CHANGELOG}: the entries are now under ${version}."
report true "${version}"
