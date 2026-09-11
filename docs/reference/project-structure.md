# Project structure

What each package is for and which way the dependencies point. For how a
message moves through the system, see `AGENTS.md`.

## Layout

    cmd/teanode-server/     the server's entry point
    cmd/teanode/            the client's entry point
    internal/               all real code, including both programs' subcommands
    web/                    dashboard source, built into internal/frontend/static
    deploy/                 docker compose files and the image's Dockerfile
    docs/                   see docs/decisions/20260818-documentation-layout.md
    vendor/                 vendored dependencies, committed

## Packages

**`internal/config`** — the configuration itself. Typed structs, a validator
that reports every problem at once with the path of each, and the `Store`
interface, which hands out immutable snapshots and applies changes as a whole.
Everything an operator can set lives here. Depends on nothing but utilities,
so any package may import it — which is why the store that persists to
PostgreSQL is not in here.

**`internal/db`** — PostgreSQL, through GORM: one interface per model —
domains with their aliases and credentials, users, roles, groups, mailboxes
with their folders and items, identities, mail, deliveries, reports,
templates — each change to an administrative object audited in the same
transaction, and the `configuration` table, which holds settings only. The
domain table's secrets are sealed with the server secret. Migrations live in
`internal/db/migrations`, forward and reverse.

**`internal/access`** — who may do what. Seeds the roles and groups a new
server starts with, resolves a user's effective permissions from their
groups, keeps every account's mailbox, signs a mail program in with an app
password, binds an identity-provider subject to an account and reconciles
the groups the provider claims, and is the rescue path when nobody can sign
in.

**`internal/imap`** — mailboxes to mail programs, over `go-imap/v2`. A
folder's UIDs and modseqs are its own, flags are the item's, the message is
read from storage when asked for, and an idling session is woken by the
database's `folder_changed` notification. Nothing is held in memory that
another instance would need.

**`internal/sso`** — signing in through an OpenID Connect provider: the
authorization-code flow with PKCE, a signed and expiring state, and a client
that will not talk to a private address. `internal/api/v1api/apisso` is the
two HTTP paths the browser passes through.

**`internal/bootstrap`** — what the environment says: how to reach the
database, which instance this process is, and — on a first run against an
empty database — what kind of server to create. Small on purpose. Anything
here is per-process and needs a restart to change, where anything in the
database is shared and does not.

**`internal/spamfilter`** — the seam between the mail path and whatever
scores a message. Two things satisfy it: an adapter for an external
SpamAssassin daemon, and the filter below. `Message` carries what the server
already established — the authentication results, the confirmed reverse DNS
name, the parsed message — so that a filter reads them rather than working
them out again.

**`internal/strainer`** — the built-in spam filter. Named for the thing that
holds the leaves back when you pour; it is not SpamAssassin and does not
claim to be. Four sources, each switchable: the signals the server already
has, public block lists over ordinary DNS, a classifier trained on this
server's own mail, and the published pattern rules.

    strainer.go             the checks that read what the server already knows
    dns.go                  block list lookups, cached, and their refusal codes
    bayes.go                the classifier, and teaching it
    rules.go                the published rule format, parsed and evaluated
    meta.go                 the expression language rules combine each other in
    ruleload.go             keeping the parsed rules in step with the database

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

    apigraph/   the GraphQL endpoint, which is the whole management API,
                and the two multipart routes that put files on a draft.
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

**`internal/llm`** — talking to language models, knowing nothing about mail:
provider-neutral chat types with tool calls and streaming; clients for the
OpenAI-compatible API (which is also Ollama, vLLM, OpenRouter and the rest),
Anthropic's and Gemini's, over `net/http`; a registry built from the `agent`
section that resolves `provider:model` names and picks a model per kind of
work; `Extract`, which turns a model's answer into a typed value after
repairing the JSON. Nothing is constructed when agents are off: `Open`
returns nil.

**`internal/agent`** — the personal agent. A queue of jobs in the database
claimed with `SKIP LOCKED` and a worker that runs them: triage (`triage.go`),
summaries, drafts, the reply that answers on the person's behalf and the
send that follows the hold, embeddings for search by meaning, backfills. The
delivery hook (`mx.AgentHook`) only queues; no model is ever called in the
SMTP path. `ask.go` is the loop that talks to the person: the prompt in
layers, overlays rebuilt each round, compaction, and the confirmation pause
that a destructive or outward tool never gets past on its own. The tools
live under `agent/tools/`: the kit (`tools`) says what a tool is — a
family, a risk class, a schema, a run reached through the context — and
holds the catalog, filtered by the person's permissions and the operator's
policy; every tool is a package of its own beside it (`tools/datetime`,
`tools/mailread`, `tools/browser`, one per family where the tools share a
body, such as `tools/rule` and `tools/domain`), registering from its
`init`; `tools/all` imports them all. Tools reach the server only through
`Operations`, which the API package implements by executing its own
operations as the person. The adapter that makes tools of a connected
server's tools stays in `agent`, since it is not a tool but a bridge to
many. Prompts are templates under `prompts/`, with golden files in
`testdata/prompts`. How each part actually works — the loop and its rounds,
the prompt's layers, compaction, the event feed and what crosses instances,
memory and its vectors, the budgets — is written down in
`docs/subsystems/`.

**`internal/mcp`** — a client for servers that speak the Model Context
Protocol: JSON-RPC over streamable HTTP or a subprocess's standard streams,
tool discovery and calls, and OAuth 2.1 with PKCE for the servers that want
a person's authorization. Only tools are consumed; what a server answers is
data. Tests speak to in-process servers.

**`internal/computer`** — the person's own computer as their agent reaches
it: the program `teanode computer` runs there, which signs in with the
person's token, keeps a websocket to the server and answers the shell and
filesystem tools' requests as that person, anywhere on the machine; and the
rule the server applies, which asks the person first for what changes the
machine or reaches off it and refuses nothing outright. The rule is the
server's alone: the program runs what it is sent, the way a terminal does
what the person at it types. It is their own
assistant on their own computer, so it is not confined to a sandbox; the
guard is the card they answer, not a directory. Tests speak to a fake
connection. `docs/subsystems/devices.md` has the protocol and the rules.

**`internal/browser`** — a DevTools client for the Chrome the operator runs
beside the server: an isolated context per run, the page read as a tree the
model can point into, click, type, scroll, wait, screenshot, with every
navigation and request through the same address guard as fetching a page
and downloads refused. Nothing launches Chrome; tests speak to a fake.

**`internal/models`** — plain structs shared between `db`, `api` and `mx`. No
behavior beyond enum helpers.

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

`internal/cmd/server` depends on everything. `config` depends on `db`, for
the settings store, and `db` on `models`; `access`, `mx`, `imap`, `sso` and
the API depend on `db` and `config` and not on each other, except that `mx`
and the API both use `access` for the checks they share.
