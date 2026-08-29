#!/usr/bin/env bash
#
# Build the release binaries.
#
#     .github/scripts/release.sh <version> <output-directory>
#
# Produces a static binary per platform plus a checksum file. The dashboard has
# to be built first: the Go build embeds internal/frontend/static, and without
# it the release would ship a server with no interface.

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/../.."

if [[ $# -ne 2 ]]; then
  echo "usage: $0 <version> <output-directory>" >&2
  exit 1
fi

readonly VERSION="${1#v}"
readonly OUTPUT="$2"
readonly COMMIT="$(git rev-parse HEAD)"

readonly PLATFORMS=(
  "linux/amd64"
  "linux/arm64"
)

if [[ ! -f internal/frontend/static/index.html ]]; then
  echo "ERROR: the dashboard has not been built; run 'make web' first" >&2
  exit 1
fi

mkdir -p "${OUTPUT}"

for platform in "${PLATFORMS[@]}"; do
  goos="${platform%%/*}"
  goarch="${platform##*/}"
  binary="${OUTPUT}/teanode-${goos}-${goarch}"

  echo "building ${binary}"
  CGO_ENABLED=0 GOOS="${goos}" GOARCH="${goarch}" go build -mod=vendor \
    -ldflags "-s -w -extldflags \"-static\" \
      -X github.com/ziyan/teanode/internal/version.version=${VERSION} \
      -X github.com/ziyan/teanode/internal/version.commit=${COMMIT}" \
    -o "${binary}" .
done

# One checksum file covering everything, so a download can be verified.
( cd "${OUTPUT}" && sha256sum teanode-* > SHA256SUMS )

echo
ls -la "${OUTPUT}"
