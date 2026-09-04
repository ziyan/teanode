#!/usr/bin/env bash
#
# Turn whatever has piled up since the last release into one: work out the next
# version, write the entries under it, and leave the changelog ready for the
# next change.
#
#     .github/scripts/cut-release.sh              # say what would happen
#     .github/scripts/cut-release.sh --write      # rewrite CHANGELOG.md
#     .github/scripts/cut-release.sh --write --major
#
# Entries reach a release by two roads and both are open: written into a pull
# request description, where they are reviewed alongside the change they
# describe, or written straight into the Unreleased section by somebody
# committing to main. Taking only one would quietly drop work.
#
# It does not touch git beyond reading it. Deciding what the version is and
# writing it down is one thing; committing, tagging and pushing is another, and
# the workflow does that with the credentials for it. Keeping them apart means
# this can be run on a laptop to see what would happen, which is the first
# thing anybody wants from a release tool.
#
# The version comes from the entries:
#
#   - "Added" or "Removed" means a new minor version
#   - anything else means a new patch version
#   - --major means a new major version, and is never inferred: deciding that a
#     change breaks whoever is running this is a judgement a person makes
#   - no tags at all means this is the first release, 0.1.0
#
# Nothing to release is not a failure. Most pushes are a refactor, a test or a
# comment, and for those it says so and exits 0.

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/../.."

readonly CHANGELOG="CHANGELOG.md"
readonly PARSER=".github/scripts/pull-request-entries.awk"

# The order sections appear in, which is Keep a Changelog's order.
readonly KINDS=(Added Changed Deprecated Removed Fixed Security)

write=0
major=0
for argument in "$@"; do
  case "${argument}" in
    --write) write=1 ;;
    --major) major=1 ;;
    *) echo "unknown argument: ${argument}" >&2; exit 1 ;;
  esac
done

work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT
mkdir -p "${work}/entries"

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

# --- what is already in the changelog -----------------------------------------

written="$(awk '
  /^## / {
    if (printing) { exit }
    if (index($0, "[Unreleased]") > 0) { printing = 1; next }
  }
  printing { print }
' "${CHANGELOG}")"

# --- what the pull requests said ----------------------------------------------

# The highest version tag, not the most recent one: a tag pushed out of order
# must not make the next release go backwards.
latest="$(git tag --list 'v*' | sed 's/^v//' | sort -V | tail -1)"

collected=0
collect_from_pull_requests() {
  if [[ -z "${GITHUB_REPOSITORY:-}" ]]; then
    echo "Not reading pull request descriptions: no GITHUB_REPOSITORY in the environment." >&2
    return 0
  fi
  if ! command -v gh >/dev/null 2>&1; then
    echo "Not reading pull request descriptions: gh is not installed." >&2
    return 0
  fi
  if [[ -z "${GH_TOKEN:-}${GITHUB_TOKEN:-}" ]]; then
    # Said out loud, because silence here hides a broken release: with no
    # token no descriptions are read, so there are no entries, and a release
    # with no entries is a legitimate outcome that fails nothing.
    echo "Not reading pull request descriptions: no GH_TOKEN or GITHUB_TOKEN; a workflow step needs one passed to it explicitly." >&2
    return 0
  fi

  local span="HEAD"
  [[ -n "${latest}" ]] && span="v${latest}..HEAD"

  # A squashed merge ends its subject with the number in brackets; a merge
  # commit says it differently. Both are looked for.
  local numbers
  numbers="$(git log --reverse --format='%s' "${span}" |
    sed -nE -e 's/.*\(#([0-9]+)\)[[:space:]]*$/\1/p' -e 's/^Merge pull request #([0-9]+) .*/\1/p' |
    awk '!seen[$0]++')"

  local number body labels
  for number in ${numbers}; do
    if ! body="$(gh api "repos/${GITHUB_REPOSITORY}/pulls/${number}" --jq '.body // ""' 2>/dev/null)"; then
      # One unreadable pull request is not a reason to release nothing.
      echo "Cannot read pull request #${number}; skipping it." >&2
      continue
    fi
    labels="$(gh api "repos/${GITHUB_REPOSITORY}/pulls/${number}" --jq '.labels[].name' 2>/dev/null || true)"
    if grep -qxF 'no changelog' <<<"${labels}"; then
      echo "#${number} is labelled 'no changelog'; skipping it." >&2
      continue
    fi
    printf '%s\n' "${body}" | awk -v number="${number}" -v outdir="${work}/entries" -f "${PARSER}"
    collected=$((collected + 1))
  done
}
collect_from_pull_requests

# --- put them together ---------------------------------------------------------

# What was written by hand is used exactly as it was written, headings, prose,
# wrapped lines and all. Somebody who took the trouble to describe a change in
# the changelog itself has said more about it than a pull request summary, and
# reformatting it would lose the difference — an entry that runs to two
# paragraphs is the one most worth keeping whole.
#
# Entries collected from pull requests are added under their own headings after
# it. A kind can therefore appear twice in one release, which is untidy and is
# the cheaper of the two mistakes.
collected_body=""
for kind in "${KINDS[@]}"; do
  [[ -s "${work}/entries/${kind}" ]] || continue
  collected_body+=$'\n'"### ${kind}"$'\n\n'"$(cat "${work}/entries/${kind}")"$'\n'
done

if [[ -z "$(tr -d '[:space:]' <<<"${written}${collected_body}")" ]]; then
  echo "Nothing under [Unreleased] and nothing in the pull requests merged since ${latest:-the beginning}, so there is nothing to release."
  report false
  exit 0
fi

section="$(printf '%s%s' "${written}" "${collected_body}")"

if [[ -z "${latest}" ]]; then
  version="0.1.0"
  reason="the first release"
else
  IFS=. read -r current_major current_minor current_patch <<<"${latest}"
  if (( major )); then
    version="$((current_major + 1)).0.0"
    reason="asked for by hand"
  elif [[ -s "${work}/entries/Added" || -s "${work}/entries/Removed" ]] ||
       grep -qE '^### (Added|Removed)' <<<"${written}"; then
    version="${current_major}.$((current_minor + 1)).0"
    reason="something was added or removed"
  else
    version="${current_major}.${current_minor}.$((current_patch + 1))"
    reason="fixes and changes only"
  fi
fi

echo "Next version: ${version} (${reason}; the last tag was ${latest:-none}, and ${collected} pull request(s) were read)."

if (( ! write )); then
  echo "Nothing was written. Pass --write to date the changelog."
  report true "${version}"
  exit 0
fi

today="$(date -u +%Y-%m-%d)"
{
  # Everything above the Unreleased heading, then an empty Unreleased ready for
  # the next change, then this release.
  awk '/^## \[Unreleased\]/ { exit } { print }' "${CHANGELOG}"
  printf '## [Unreleased]\n\n## [%s] - %s\n' "${version}" "${today}"
  # A blank line after the entries, or the heading of the release before this
  # one sits directly under the last bullet of this one. Command substitution
  # has already eaten any trailing newline the section had, so this is exactly
  # one blank line and never two.
  printf '%s\n\n' "${section}"
  # Everything from the release before this one onwards, untouched.
  awk '
    /^## \[Unreleased\]/ { seen = 1; next }
    seen && /^## \[/ { printing = 1 }
    printing { print }
  ' "${CHANGELOG}"
} >"${CHANGELOG}.next"
mv "${CHANGELOG}.next" "${CHANGELOG}"

echo "Wrote ${CHANGELOG}: the entries are now under ${version}."
report true "${version}"
