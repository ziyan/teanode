#!/usr/bin/env bash
#
# Build the release binaries.
#
#     .github/scripts/release.sh <version> <output-directory>
#
# Produces a static teanode-server per server platform, a static teanode (the
# client) per client platform, and one checksum file. The dashboard has to be
# built first: the server build embeds internal/frontend/static, and without it
# the release would ship a server with no interface. The client is also built
# for macOS, because it is the half that belongs on a laptop.

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/../.."

if [[ $# -ne 2 ]]; then
  echo "usage: $0 <version> <output-directory>" >&2
  exit 1
fi

readonly VERSION="${1#v}"
readonly OUTPUT="$2"
readonly COMMIT="$(git rev-parse HEAD)"

readonly SERVER_PLATFORMS=(
  "linux/amd64"
  "linux/arm64"
)

readonly CLIENT_PLATFORMS=(
  "linux/amd64"
  "linux/arm64"
  "darwin/amd64"
  "darwin/arm64"
)

if [[ ! -f internal/frontend/static/index.html ]]; then
  echo "ERROR: the dashboard has not been built; run 'make web' first" >&2
  exit 1
fi

mkdir -p "${OUTPUT}"

build() {
  local program="$1" platform="$2"
  local goos="${platform%%/*}" goarch="${platform##*/}"
  local binary="${OUTPUT}/${program}-${goos}-${goarch}"

  echo "building ${binary}"
  CGO_ENABLED=0 GOOS="${goos}" GOARCH="${goarch}" go build -mod=vendor \
    -ldflags "-s -w -extldflags \"-static\" \
      -X github.com/ziyan/teanode/internal/version.version=${VERSION} \
      -X github.com/ziyan/teanode/internal/version.commit=${COMMIT}" \
    -o "${binary}" "./cmd/${program}"
}

for platform in "${SERVER_PLATFORMS[@]}"; do
  build teanode-server "${platform}"
done
for platform in "${CLIENT_PLATFORMS[@]}"; do
  build teanode "${platform}"
done

# One checksum file covering everything, so a download can be verified.
( cd "${OUTPUT}" && sha256sum teanode-* > SHA256SUMS )

echo
ls -la "${OUTPUT}"
