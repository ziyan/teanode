#!/usr/bin/env bash
# Builds the signed release of the Android app and sends it to the testers.
#
#   android/scripts/distribute.sh "What changed in this build"
#   android/scripts/distribute.sh --dry-run "What changed in this build"
#
# Needs, in SECRETS_DIRECTORY (default ~/.secrets/teanode-android):
#   teanode-release.jks and store-password.txt  the release key
#   teanode-distribute.json                      a service account allowed to upload
#   google-services.json                         the Firebase app's configuration
#   install-url.txt                              the testers' invite link
# and the Firebase CLI as FIREBASE_BINARY or on PATH.
#
# The build number is the commit count, so each build is newer than the last.
# After a successful upload, and only then, it publishes {"version","build",
# "installUrl"} as the asset android.json of the GitHub release
# android-update-channel, which servers read to tell the app a newer build
# exists.
set -euo pipefail

SECRETS_DIRECTORY="${SECRETS_DIRECTORY:-${HOME}/.secrets/teanode-android}"
FIREBASE_BINARY="${FIREBASE_BINARY:-firebase}"
TESTER_GROUP="${TESTER_GROUP:-testers}"
UPDATE_CHANNEL_RELEASE="android-update-channel"
IS_DRY_RUN=false
if [[ "${1:-}" == "--dry-run" ]]; then
    IS_DRY_RUN=true
    shift
fi
RELEASE_NOTES="${1:-}"
if [[ -z "${RELEASE_NOTES}" ]]; then
    echo "usage: android/scripts/distribute.sh [--dry-run] \"release notes\"" >&2
    exit 1
fi

cd "$(git rev-parse --show-toplevel)"
if [[ -n "$(git status --porcelain --untracked-files=no)" ]]; then
    echo "distribute: commit your changes first, so the build matches a commit" >&2
    exit 1
fi
ACCOUNT_FILE="${SECRETS_DIRECTORY}/teanode-distribute.json"
for REQUIRED_FILE in "${ACCOUNT_FILE}" "${SECRETS_DIRECTORY}/teanode-release.jks" \
    "${SECRETS_DIRECTORY}/store-password.txt" "${SECRETS_DIRECTORY}/google-services.json" \
    "${SECRETS_DIRECTORY}/install-url.txt"; do
    if [[ ! -s "${REQUIRED_FILE}" ]]; then
        echo "distribute: ${REQUIRED_FILE} is missing or empty" >&2
        exit 1
    fi
done
INSTALL_URL="$(tr -d '[:space:]' < "${SECRETS_DIRECTORY}/install-url.txt")"
if [[ "${INSTALL_URL}" != https://* ]]; then
    echo "distribute: ${SECRETS_DIRECTORY}/install-url.txt does not hold an https link" >&2
    exit 1
fi

# The configuration is copied in for the build and taken out again, so it is
# never committed by accident.
cp "${SECRETS_DIRECTORY}/google-services.json" android/app/google-services.json
trap 'rm -f android/app/google-services.json' EXIT

VERSION_CODE="$(git rev-list --count HEAD)"
(cd android && TEANODE_ANDROID_SIGNING_DIRECTORY="${SECRETS_DIRECTORY}" \
    ./gradlew --no-daemon -q :app:assembleRelease -PteanodeVersionCode="${VERSION_CODE}")
APK_FILE=android/app/build/outputs/apk/release/app-release.apk
APP_ID="$(python3 -c 'import json, sys; print(json.load(open(sys.argv[1]))["client"][0]["client_info"]["mobilesdk_app_id"])' \
    android/app/google-services.json)"
VERSION_NAME="$(sed -n 's/.*versionName = "\(.*\)".*/\1/p' android/app/build.gradle.kts | head -1)"

echo "distribute: built ${VERSION_NAME} (${VERSION_CODE}) at commit $(git rev-parse --short HEAD)"
if [[ "${IS_DRY_RUN}" == true ]]; then
    echo "distribute: dry run, nothing was uploaded"
    exit 0
fi
GOOGLE_APPLICATION_CREDENTIALS="${ACCOUNT_FILE}" "${FIREBASE_BINARY}" appdistribution:distribute "${APK_FILE}" \
    --app "${APP_ID}" --groups "${TESTER_GROUP}" --release-notes "${RELEASE_NOTES}"

# Only reached when the upload succeeded (set -e stops the script otherwise).
FEED_DIRECTORY="$(mktemp -d)"
python3 - "${VERSION_NAME}" "${VERSION_CODE}" "${INSTALL_URL}" "${FEED_DIRECTORY}/android.json" <<'PYTHON'
import json
import sys

version, build, install_url, target = sys.argv[1:]
with open(target, "w") as feed:
    json.dump({"version": version, "build": int(build), "installUrl": install_url}, feed)
    feed.write("\n")
PYTHON
if ! gh release view "${UPDATE_CHANNEL_RELEASE}" > /dev/null 2>&1; then
    gh release create "${UPDATE_CHANNEL_RELEASE}" --prerelease --title "Android update channel" \
        --notes "The newest Android build for testers. Kept up to date by android/scripts/distribute.sh."
fi
gh release upload "${UPDATE_CHANNEL_RELEASE}" "${FEED_DIRECTORY}/android.json" --clobber
rm -rf "${FEED_DIRECTORY}"
echo "distribute: sent build ${VERSION_CODE} to ${TESTER_GROUP} and published the update feed"
