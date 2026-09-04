# Build the dashboard, then the server with the dashboard compiled into it
# and the client beside it, then ship both static binaries on a base with no
# shell and no package manager.

FROM node:22 AS dashboard
WORKDIR /src
# Install dependencies first so that a source-only change does not repeat it.
COPY web/package.json web/package-lock.json ./web/
RUN cd web && npm ci --no-audit --no-fund
COPY web ./web
# The build writes into internal/frontend/static, which the Go build embeds.
RUN mkdir -p internal/frontend/static && cd web && npm run build

FROM golang:1.25 AS server
# Passed by the release workflow so the binary can report what it is. Without
# them "teanode-server version" says 0.0.0-dev, which is useless in a bug
# report.
ARG VERSION=0.0.0-dev
ARG COMMIT=unknown
WORKDIR /src
COPY . .
COPY --from=dashboard /src/internal/frontend/static ./internal/frontend/static
RUN CGO_ENABLED=0 go build -mod=vendor \
    -ldflags "-s -w -extldflags \"-static\" \
      -X github.com/ziyan/teanode/internal/version.version=${VERSION} \
      -X github.com/ziyan/teanode/internal/version.commit=${COMMIT}" \
    -o /teanode-server ./cmd/teanode-server \
 && CGO_ENABLED=0 go build -mod=vendor \
    -ldflags "-s -w -extldflags \"-static\" \
      -X github.com/ziyan/teanode/internal/version.version=${VERSION} \
      -X github.com/ziyan/teanode/internal/version.commit=${COMMIT}" \
    -o /teanode ./cmd/teanode

# The :nonroot tag runs as uid 65532 rather than root. Binding 25, 80, 443 and
# 587 still needs CAP_NET_BIND_SERVICE, which the compose files grant — the
# capability is the whole of what root was being used for, and granting just
# that is smaller than granting all of it.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=server /teanode-server /usr/local/bin/teanode-server
# The client too, so that "docker compose exec teanode teanode user list"
# administers the server from inside its own container with nothing set up.
COPY --from=server /teanode /usr/local/bin/teanode
# Mail, the dashboard, and the ACME http-01 challenge.
EXPOSE 25 80 443 587
ENTRYPOINT ["/usr/local/bin/teanode-server"]
CMD ["run"]
