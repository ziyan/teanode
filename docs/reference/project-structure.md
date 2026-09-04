# Project structure

What each package is for and which way the dependencies point. For how a
message moves through the system, see `AGENTS.md`.

## Layout

    cmd/teanode-server/     the server's entry point
    cmd/teanode/            the client's entry point
    internal/               all real code, including both programs' subcommands
    web/                    dashboard source, built into internal/frontend/static
    deploy/                 systemd unit, docker compose, example configuration
    docs/                   see docs/decisions/20260818-documentation-layout.md
    vendor/                 vendored dependencies, committed

## Packages

**`internal/config`** — the configuration itself. Typed structs, a validator
that reports every problem at once with the path of each, and the `Store`
interface, which hands out immutable snapshots and applies changes as a whole.
Everything an operator can set lives here. Depends on nothing but utilities,
so any package may import it — which is why the store that persists to
PostgreSQL is not in here.

**`internal/configdb`** — `config.Store` backed by PostgreSQL. Maps the
configuration onto tables and back, resolves concurrent writes with a version
row, and polls it so an instance notices a change made by another one. Its own
package because it imports both `config` and `db`, and `db` imports `config`.

**`internal/bootstrap`** — what the environment says: how to reach the
database, which instance this process is, and — on a first run against an
empty database — what kind of server to create. Small on purpose. Anything
here is per-process and needs a restart to change, where anything in the
database is shared and does not.

**`internal/mx`** — the mail path, and the only package that decides what
happens to a message.

    exchange.go             wiring, and HandleEnvelope which picks the path
    exchange_incoming.go    inbound: authenticate, store, match aliases
    exchange_outgoing.go    submission from an authenticated credential
    exchange_delivery.go    one delivery attempt, retries, bounces on failure
    exchange_bounce.go      delivery status notifications arriving back
    exchange_dmarc.go       aggregate DMARC reports arriving back
    exchange_usage.go       in-memory counters flushed to the database
    exchange_utils.go       header formatting, the parallel authenticator

**`internal/db`** — PostgreSQL through GORM. Holds only data that grows without
bound: mail, deliveries, DMARC reports, usage counters, templates and layouts.
One file per entity, each defining a GORM model separate from the shared struct
in `internal/models`, so the storage shape can change without changing the API.

**`internal/api`** — what every API version shares: error values, the request
context, and the paths. Deliberately depends on almost nothing, so the
versioned packages and `internal/web` can all import it.

**`internal/api/v1api`** — version 1, mounted at `/api/v1`. A composition of
three parts:

    apigraph/   the GraphQL endpoint, which is the whole management API.
                Generated from Go types by reflection in internal/util/graphapi.
                Queries read the database; mutations that change configuration
                go through config.Store and end up in the configuration
                tables, where every instance sees them.
    apisend/    POST /api/v1/send/{domain}/{template}, authenticated with an
                SMTP credential, for an application that would rather not
                speak SMTP. Takes "locale" beside "variables", and renders
                the closest translation the template has.
    apimail/    the raw .eml of a stored message and its attachments, which
                are files rather than JSON and so are not GraphQL.

**`internal/client`** — the other side of that API, used by the command line
client: one file per resource with the queries written out, and
`introspect.go`, which reads the schema from the server and builds a query
for any operation — how `teanode api` reaches all of them without a hand
written command each.

**`internal/cmd`** — the client's subcommands, one file per group, and the
helpers both programs share: reaching a server (`client.go`), the saved
profiles (`profile.go`), the browser sign-in (`loopback.go`), tables and
prompts. `internal/cmd/server` is the server's own subcommands: `run`, and
the few operations that write the database directly. See
`docs/reference/command-line.md`, and `docs/decisions/20260903-two-binaries.md`
for why they are two programs.

**`internal/web`** — HTTP server, routing and middlewares. Knows nothing about
mail.

**`internal/dns`** — periodically checks that each configured domain's DNS
records are published, and reports what is missing. Advisory only; see
`docs/decisions/20260818-dns-verification-is-advisory.md`.

**`internal/mailer`** — renders templates and sends mail on the server's own
behalf: a message composed in the dashboard, a template rendered for the send
endpoint. `Render` chooses a translation by locale and fills a template in;
`Send` assembles a message from text, HTML and attachments and hands it to
`mx` as outgoing mail from the domain.

**`internal/models`** — plain structs shared between `db`, `api` and `mx`. No
behaviour beyond enum helpers.

**`internal/util`** — protocol implementations, each independently testable and
free of project-specific assumptions:

    smtpd       SMTP server: the conversation, STARTTLS, AUTH, limits
    smtpc       SMTP client used for delivery
    mailparse   splitting, header decoding, address signing
    dkim        DomainKeys Identified Mail signing and verification
    spf         Sender Policy Framework evaluation
    dmarc       DMARC policy lookup and aggregate report parsing
    arc         Authenticated Received Chain, which survives forwarding
    authres     Authentication-Results header formatting and parsing
    dsn         delivery status notification parsing
    autoacme    ACME client with http-01, tls-alpn-01 and dns-01 solvers
    resolver    DNS resolver with caching
    clamav      optional virus scanning
    spamc       optional SpamAssassin scoring
    geoip       optional sender geolocation
    dropper     connection drop list
    graphapi    GraphQL schema generation from Go types
    atomicfile  write-to-temp-then-rename, used for every sensitive file
    security    identifiers, credential encoding, signing
    periodic    a loop that runs a function on an interval

## Dependency direction

`internal/cmd/server` depends on everything. `configdb` depends on `config`
and `db`; nothing depends on `configdb` except `internal/cmd`, which is what
keeps the choice of where configuration is stored out of everything that
reads it. `api`, `mx`, `dns`
and `mailer` depend on `config`, `db`, `models` and `util`. `util` packages depend only on each other and the
standard library. Nothing in `util` may import `config`, `db` or `models`: they
are meant to be liftable into a separate library, and several of them are the
reason this project is worth publishing.
