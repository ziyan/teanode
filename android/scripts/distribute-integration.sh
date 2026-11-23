#!/usr/bin/env bash
# Rebuilds the integration branch (main and every open pull request), pushes
# it, and sends its Android build to the testers, so testers always have
# every change still in review.
#
#   android/scripts/distribute-integration.sh "What changed in this build"
#
# Works in its own clone, INTEGRATION_DIRECTORY (default
# ~/.cache/claude-tmp/teanode-integration), never in a checkout somebody is
# editing. A pull request that does not merge cleanly is left out and named.
set -euo pipefail

RELEASE_NOTES="${1:?usage: distribute-integration.sh \"release notes\"}"
INTEGRATION_DIRECTORY="${INTEGRATION_DIRECTORY:-${HOME}/.cache/claude-tmp/teanode-integration}"
REPOSITORY="${REPOSITORY:-git@github.com:ziyan/teanode.git}"

if [[ ! -d "${INTEGRATION_DIRECTORY}/.git" ]]; then
    git clone -q "${REPOSITORY}" "${INTEGRATION_DIRECTORY}"
fi
cd "${INTEGRATION_DIRECTORY}"
git fetch -q origin --prune
git checkout -q -B integration origin/main
LEFT_OUT=()
for BRANCH in $(gh pr list --state open --base main --limit 200 --json headRefName --jq '.[].headRefName'); do
    if git merge -q --no-edit "origin/${BRANCH}" > /dev/null 2>&1; then
        echo "integration: merged ${BRANCH}"
    else
        git merge --abort
        LEFT_OUT+=("${BRANCH}")
    fi
done
git push -q -f origin integration
if (( ${#LEFT_OUT[@]} > 0 )); then
    echo "integration: left out, they do not merge cleanly: ${LEFT_OUT[*]}" >&2
fi
android/scripts/distribute.sh "${RELEASE_NOTES}"
echo "integration: build $(git rev-list --count HEAD) from integration $(git rev-parse --short HEAD)"
