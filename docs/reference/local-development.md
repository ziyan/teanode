# Local development

How to get a working TeaNode build and a database to point it at. For what the
pieces are and how they fit together, see `AGENTS.md` in the repository root. For
the conventions your change has to follow, see `docs/coding/coding-standards.md`.

## Prerequisites

- Go 1.25 or newer
- Node 20 or newer, for the dashboard
- Docker, for the PostgreSQL the tests use
- Optional: `gogolint`, for the local naming checks that `make lint` runs
  (`go install github.com/ziyan/gogolint@latest`)

## Build and test

    make build          # build/teanode-server and build/teanode, the client
    make web            # build the dashboard into internal/frontend/static
    make                # format, build, test
    make test           # tests; starts a PostgreSQL container automatically
    make lint           # golangci-lint plus gogolint
    make lint-ci        # only what CI runs

To run one package's tests:

    go test -mod=vendor -v ./internal/util/dkim -run TestVerify

Dependencies are vendored. After changing `go.mod`, run `go mod tidy && go mod
vendor` and commit the `vendor/` change with it.

## The short way

    make dev

That starts PostgreSQL and MinIO, writes `dev/.env` if it is missing, sets the
database up from it, generates a self-signed certificate, and runs the server.
`make dev-frontend` runs the dashboard's own dev server beside it.

Everything below is the same thing by hand.

## PostgreSQL

`make dev-up` starts one, and the tests start their own. A server you run
entirely by hand needs one:

    docker run --restart always --name teanode-postgres \
      --env POSTGRES_DB=teanode \
      --env POSTGRES_USER=teanode \
      --env POSTGRES_PASSWORD=teanode \
      --publish 127.0.0.1:5432:5432 \
      -d postgres

A shell on it:

    docker exec -it teanode-postgres psql -U teanode teanode

To wipe the schema and let the server rebuild it from migrations:

    docker exec -it teanode-postgres psql -U teanode teanode \
      -c 'DROP SCHEMA public CASCADE; CREATE SCHEMA public;'

## Running a development server

Configuration lives in the database, and the environment says where that is.
`scripts/dev-config.bash` writes a `dev/.env` that is safe on a laptop:

- `TEANODE_LISTEN_SMTP_INCOMING=127.0.0.1:10025` and
  `TEANODE_LISTEN_SMTP_OUTGOING=127.0.0.1:10587`, so you do not need root to
  bind ports 25 and 587
- `TEANODE_LISTEN_HTTP=127.0.0.1:10081`, `TEANODE_LISTEN_HTTPS=` (empty, which
  turns the HTTPS listener off)
- `TEANODE_TLS_ACME_ENABLED=false`, since a development box has no public name
- `TEANODE_SMTP_DISABLE_SEND=true`, so a mistake in an alias cannot mail a
  stranger
- `TEANODE_S3_*` pointing at the MinIO `make dev-up` starts

Those describe the server only on the first run against an empty database.
After that the database holds the answer, and the server warns about any of
them that disagree. To start over, `make dev-clean`.

    set -a; . ./dev/.env; set +a
    ./build/teanode-server config init
    ./build/teanode-server tls self-signed
    ./build/teanode-server run --log-level DEBUG

With that environment in your shell, the client reaches that server over
loopback with nothing else set up: `teanode domain list`, `teanode dkim show
example.com`, `teanode user list`. From another shell, sign in instead with
`teanode auth login --url http://127.0.0.1:10081`.

Send it a message with `swaks`:

    swaks --to hello@example.com --from someone@example.net --server 127.0.0.1:10025

## Optional services

Both are off by default. Turn them on in the dashboard, or in an exported
configuration loaded back with `teanode-server config import`, only when you are
working on that code path.

### SpamAssassin

    docker run --restart always --name spamassassin \
      --publish 127.0.0.1:783:783 \
      --env 'UPDATE_PERIOD=*/15 * * * *' \
      -d tiredofit/spamassassin

Score a message by hand, which is what `internal/util/spamc` does:

    (echo -en 'SYMBOLS SPAMC/1.5\r\n\r\n'; cat message.eml) | nc -q0 localhost 783

    SPAMD/1.1 0 EX_OK
    Content-length: 154
    Spam: False ; -0.2 / 5.0

    DKIM_SIGNED,DKIM_VALID,FREEMAIL_FROM,HTML_MESSAGE,SPF_PASS,URIBL_BLOCKED

Set `antispam.enabled: true` and a domain's `spamFilterScoreThreshold` to use
it. Mail scoring at or above the threshold is rejected.

### ClamAV

    docker run --restart always --name clamav \
      --publish 127.0.0.1:3310:3310 \
      --volume /var/lib/clamav \
      -d clamav/clamav

Check it is alive, and feed it the EICAR test string, which every scanner
reports as a virus without being one:

    echo 'nSTATS' | nc -q0 localhost 3310

    (echo -en 'nINSTREAM\n\0\0\0\x44'; \
     echo -n 'X5O!P%@AP[4\PZX54(P^)7CC)7}$EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*'; \
     echo -en '\0\0\0\0') | nc -q0 localhost 3310

Set `antivirus.enabled: true` to use it.

## Binding the real mail ports without root

In production the systemd unit grants `CAP_NET_BIND_SERVICE`, so the server
binds 25, 80, 443 and 587 as an unprivileged user. If you would rather run on
high ports and redirect, this also works:

    iptables  -t nat -A PREROUTING -i eth0 -p tcp --dport 25  -j REDIRECT --to-port 10025
    iptables  -t nat -A PREROUTING -i eth0 -p tcp --dport 587 -j REDIRECT --to-port 10587
    ip6tables -t nat -A PREROUTING -i eth0 -p tcp --dport 25  -j REDIRECT --to-port 10025
    ip6tables -t nat -A PREROUTING -i eth0 -p tcp --dport 587 -j REDIRECT --to-port 10587
