# Build the dashboard, then the server with the dashboard compiled into it
# and the client beside it. The runtime includes local media readers, with no
# shell or package manager.

FROM node:22 AS dashboard
WORKDIR /src
# Install dependencies first so that a source-only change does not repeat it.
COPY web/package.json web/package-lock.json ./web/
COPY web/scripts/patch-braces.cjs ./web/scripts/
RUN cd web && npm ci --no-audit --no-fund
COPY web ./web
# The build writes into internal/frontend/static, which the Go build embeds.
RUN mkdir -p internal/frontend/static && cd web && npm run build

FROM golang:1.26.9 AS server
# Passed by the release workflow so the binary can report what it is. Without
# them "teanode-server version" says 0.0.0-dev, which is useless in a bug
# report.
ARG VERSION=0.0.0-dev
ARG COMMIT=unknown
WORKDIR /src
COPY . .
COPY --from=dashboard /src/internal/frontend/static ./internal/frontend/static
RUN CGO_ENABLED=0 go generate -mod=vendor ./internal/util/commentparse \
 && CGO_ENABLED=0 go build -mod=vendor \
    -ldflags "-s -w -extldflags \"-static\" \
      -X github.com/ziyan/teanode/internal/version.version=${VERSION} \
      -X github.com/ziyan/teanode/internal/version.commit=${COMMIT}" \
    -o /teanode-server ./cmd/teanode-server \
 && CGO_ENABLED=0 go build -mod=vendor \
    -ldflags "-s -w -extldflags \"-static\" \
      -X github.com/ziyan/teanode/internal/version.version=${VERSION} \
      -X github.com/ziyan/teanode/internal/version.commit=${COMMIT}" \
    -o /teanode ./cmd/teanode

FROM debian:trixie-slim AS media-readers
RUN apt-get update \
 && apt-get install -y --no-install-recommends ffmpeg \
 && mkdir -p /reader-root/usr/local/bin /reader-root/usr/share/doc \
 && cp /usr/bin/ffmpeg /usr/bin/ffprobe /reader-root/usr/local/bin/ \
 && ldd /usr/bin/ffmpeg /usr/bin/ffprobe \
    | awk '/=> \// {print $3} $1 ~ /^\// && $2 ~ /^\(/ {print $1}' \
    | sed -e 's@^/lib/@/usr/lib/@' -e 's@^/lib64/@/usr/lib64/@' \
    | sort -u | xargs -r cp -L --parents -t /reader-root \
 && cp -a /usr/share/doc/. /reader-root/usr/share/doc/ \
 && rm -rf /var/lib/apt/lists/*

# The readers and their linked libraries come from the same distribution as
# the runtime. Only descriptors and pipe input are admitted by the decoder.
FROM gcr.io/distroless/base-debian13:nonroot AS runtime
COPY --from=media-readers /reader-root/ /

FROM server AS recording-test-build
RUN CGO_ENABLED=0 go test -mod=vendor -c -o /recording-tests ./internal/recording

# The image check exercises the packaged readers without networking. Decoder
# tests may skip on developer machines, but must never skip in this target.
FROM runtime AS recording-tests
COPY --from=recording-test-build /recording-tests /usr/local/bin/recording-tests
ENV TEANODE_RECORDING_DECODER_REQUIRED=1
ENTRYPOINT ["/usr/local/bin/recording-tests"]
CMD ["-test.v", "-test.run", "Audio|PrivateMedia"]

# The :nonroot tag runs as uid 65532 rather than root. Binding 25, 80, 443 and
# 587 still needs CAP_NET_BIND_SERVICE, which the compose files grant — the
# capability is the whole of what root was being used for, and granting just
# that is smaller than granting all of it.
FROM runtime AS production
COPY --from=server /teanode-server /usr/local/bin/teanode-server
# The client too, so that "docker compose exec teanode teanode user list"
# administers the server from inside its own container with nothing set up.
COPY --from=server /teanode /usr/local/bin/teanode
# Mail, the dashboard, and the ACME http-01 challenge.
EXPOSE 25 80 443 587
ENTRYPOINT ["/usr/local/bin/teanode-server"]
CMD ["run"]
