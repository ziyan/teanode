# Restructure TeaNode into a single self-hostable open-source mail server binary

> **Status.** The restructure described here is done; the cutover it leads to
> is not. The documentation layout proposed below was superseded while it was
> being carried out — there is no `docs/adr/`, no `docs/execplans/` and no
> `docs/deployment.md`; decision records live in `docs/decisions/` and
> deployment is covered by `docs/getting-started.md` and `deploy/`. The plan is
> kept as written rather than edited to match, because a plan rewritten after
> the fact stops being evidence of what was intended.

This ExecPlan is a living document. The sections `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to date as work proceeds. It is maintained in accordance with the ExecPlan requirements described in `~/.claude/PLAN.md` (a copy of those requirements is not checked into this repository; everything needed to follow this plan is contained in this file).

## Purpose / Big Picture

Today TeaNode is the private backend of a hosted email forwarding service. It is a two-directory repository (`backend/` Go, `frontend/` React) that assumes a specific AWS account, a specific Route53 hosted zone, a Postgres database that stores both configuration and mail, a Redis server, an S3 bucket, and a frontend deployed separately to S3. Nobody but the author can run it.

After this change, a person who has never seen this repository can do the following on a fresh Linux box:

    # download one file, write one config file, run it
    curl -L -o /usr/local/bin/teanode https://github.com/ziyan/teanode/releases/latest/download/teanode-linux-amd64
    chmod +x /usr/local/bin/teanode
    mkdir -p /opt/teanode
    teanode config init --output /opt/teanode/teanode.yaml
    $EDITOR /opt/teanode/teanode.yaml     # set your domain and where to forward mail
    teanode run --config /opt/teanode/teanode.yaml

and have a working mail server that accepts mail for their own domain, authenticates it (SPF, DKIM, DMARC, ARC), optionally scans it for viruses and spam, forwards it to their real mailbox, relays their outbound mail with SMTP AUTH, and serves a web dashboard on `https://mail.example.com/` where they can read the received mail *rendered as mail* — not as a JSON blob — and change every setting without touching the YAML by hand.

The binary is one statically linked executable with the web dashboard compiled into it. The only external service it requires is Postgres, and that is only because mail volume grows without bound and must stay searchable. Everything cloud-shaped — S3 archival, Route53 DNS challenges, GeoIP lookups, ClamAV, SpamAssassin — is optional and off by default.

You can see it working end to end without owning a domain: milestone acceptance below includes a local loopback test where you send a message with `swaks` to `127.0.0.1:10025` and watch it get forwarded, appear in the dashboard, and render.

## Progress

- [x] (2026-08-18) Surveyed the existing repository: 185 Go files, ~24k lines of non-vendored Go across `backend/{api,cmd,db,dns,mailer,models,mx,node,util,version,web}`, 34 TypeScript files in `frontend/`, 448 commits, 57MB of git history.
- [x] (2026-08-18) Settled the architecture questions with the repository owner. See `Decision Log`.
- [x] (2026-08-18) Wrote this ExecPlan.
- [x] (2026-08-18) Milestone 1 — Root Go module, `internal/` layout, node relay and Redis removed, build and 168 tests green. Commit `8826d09`.
- [x] (2026-08-18) Milestone 2 — `internal/config` with store, validator and atomic save; CLI on urfave/cli v3 with config/password/dkim/credential/version subcommands; 195 tests green. Commit `49f84c7`.
- [x] (2026-08-18) Milestone 3 — Database reduced to eight tables; migrations restarted at `0000` with reverse SQL. Commit `b6aa95b`.
- [x] (2026-08-18) Milestone 4 — Mail path, DNS checker and mailer read identity from configuration; verified end to end locally. Commit `b6aa95b`.
- [x] (2026-08-18) Milestone 5 — HTTP-01 and TLS-ALPN-01 solvers; Route53 optional; no AWS client unless enabled. Commit `0eb8d62`.
- [x] (2026-08-18) Milestone 6 — Dashboard authentication: accounts, bcrypt, signed session cookie. Passkeys and API tokens followed.
- [x] (2026-08-18) Milestone 7 — GraphQL API reworked: config-backed mutations, database-backed queries. Configuration later moved into the database itself; see `docs/decisions/20260824-configuration-in-the-database.md`.
- [x] (2026-08-18) Milestone 8 — New embedded dashboard with a rendered mail view, and since then the compose, template and layout pages.
- [x] (2026-08-18) Milestone 9 — Deployment: `/opt/teanode` layout, docker compose, documentation, and a deployment test that brings the whole stack up.
- [x] (2026-08-18) Milestone 10 — Project infrastructure: mulint, golangci-lint, release automation, changelog, decision records, agent-facing docs.
- [ ] Milestone 11 — History reset to a single initial commit and open-source publication. Carried into `20260903-open-source-publication.md`, which does the audit this milestone assumed.

## Surprises & Discoveries

- Observation: the frontend is not served by the Go binary at all today; `frontend/package.json` has a `deploy` script that `aws s3 sync`s the built bundle to the service's own S3 bucket. Embedding the frontend is therefore new work, not a refactor of existing serving code.
  Evidence: `frontend/package.json` `scripts.deploy`, and `backend/web/server.go` which registers only component routes and no static file handler.

- Observation: there is no secret material committed to the repository or to its history. The private keys, ACME account key, AWS credentials file and the 56MB GeoIP database all live in `backend/build/`, which is covered by `/build` in `backend/.gitignore`.
  Evidence: `git ls-files | grep -Ei 'secret|\.key$|awscred'` returns only source files whose names contain those words (`util/security/credential.go` and similar) plus vendored AWS SDK paths.

- Observation: hardcoded production identifiers *were* committed, in `backend/cmd/run/flags.go` as flag defaults: an AWS Route53 hosted zone id, a shared forwarder key, and the hosted service's own domain in a dozen places. The values are deliberately not reproduced in this document, because this document is itself published. They are gone from the working tree as of Milestone 2, which deleted `flags.go`, and they leave the history in Milestone 11. The forwarder key must be treated as compromised and rotated by the owner regardless, since it existed in a repository that is about to be published.
  Evidence: `git log -S` on the removed flag defaults in `backend/cmd/run/flags.go`, which defined `forwarder-key`, `aws-route53-zone-id`, `hosts`, `acme-email`, `smtp-server-name` and `domain-key-domain`.

- Observation: `db/database.go` calls `db.Debug()` unconditionally, so every SQL statement is logged at every log level in production. Fixed in Milestone 3's precursor: it is now behind `database.logQueries`.
  Evidence: `backend/db/database.go`, `return &database{db: db.Debug(), ...}`.

- Observation: `git add -A` after Milestone 1 staged `backend/build/` — the owner's live DKIM private keys, ACME account key, AWS credentials file, 56MB GeoIP database and captured production mail. Those files had been protected only by `/build` in `backend/.gitignore`, which Milestone 1 deleted along with the rest of `backend/`. Caught before committing; the root `.gitignore` now carries an explicit `/backend/` rule.
  Evidence: `git status --porcelain` listed `A  backend/build/awscredentials` and `A  backend/build/teanode1.key` among 2680 staged paths.
  Lesson: when a move deletes a `.gitignore`, everything it protected becomes stageable in the same commit. The directory still exists on disk and has not been touched; the owner should relocate it before it is deleted by hand.

- Observation: a relative `server.dataDirectory` resolved against the process working directory produced a silent, dangerous failure: `teanode credential add` run from one directory and `teanode credential list --show-passwords` run from another generated *two different* server secrets and therefore printed two different SMTP passwords for the same credential, neither obviously wrong.
  Evidence: the same credential printed `khhfq3pzxawkkms4rj6uitnhsuprqabg` and then `khhfq3pzxawkkms4ewhlrfryon6gufff`; the log line "generated a new server secret" appeared twice.
  Fixed by resolving relative paths against the directory holding the configuration file, covered by `TestRelativePathsResolveAgainstTheConfigurationFile` and `TestSecretIsGeneratedOnceAndReused`.

- Observation: `go build ./...` compiles Go source vendored inside `web/node_modules` — the `flatted` npm package ships `golang/pkg/flatted`. The Makefile now uses an explicit package list instead of `./...`.
  Evidence: `EMPTY web/node_modules/flatted/golang/pkg/flatted (coverage: 0.0% of statements)` in the test output.

- Observation: `internal/util/mux`, a yamux-based stream multiplexer, was referenced only by its own test once the node relay was deleted. Removed, along with the `hashicorp/yamux` dependency.
  Evidence: `grep -rn "util/mux" --include='*.go' .` matched only `internal/util/mux/mux_test.go`.

- Observation: the DSN (bounce) return path is cryptographically signed with the process secret and embedded in the envelope sender (`mailparse.SignAddress("dsn", delivery.ID, ...)`). Delivery identifiers therefore appear in outbound envelope senders, which means delivery IDs must stay stable and must not be re-generated across a migration.
  Evidence: `backend/mx/exchange_delivery.go`, and `Envelope.SpecialPrefix` / `SpecialID` handling in `backend/util/mailparse`.

## Decision Log

- Decision: The repository root becomes the Go module. `backend/` disappears; Go source moves to `main.go`, `cmd/`, and `internal/`. Frontend source moves to `web/`. The module path becomes `github.com/ziyan/teanode`.
  Rationale: the owner asked for a root-level Go project that builds one executable with the frontend embedded, matching the layout of their other project at `~/projects/teanode/teanode`. A single module also makes `go install github.com/ziyan/teanode@latest` work for anyone.
  Date/Author: 2026-08-18, Ziyan Zhou (decision), Claude (record).

- Decision: Configuration lives in one YAML file, by default `/opt/teanode/teanode.yaml`. The running server may rewrite that file atomically when configuration is changed through the web UI. Hand edits are reloaded on `SIGHUP`.
  Rationale: the owner wants everything configurable from the web UI *and* wants configuration in YAML rather than in the database. A single self-rewriting file keeps one source of truth. The accepted cost is that a UI-driven write reformats the file and loses hand-written comments; a fixed explanatory header is re-emitted on every write to keep the file self-documenting.
  Date/Author: 2026-08-18, Ziyan Zhou.

- Decision: Postgres stays, and holds only data that grows without bound and needs searching: `mail`, `delivery`, `report` (DMARC aggregate reports), the three `usage` rollup tables, and `template`/`layout`. The `domain`, `alias`, `credential`, `user`, `token` and `node` tables are removed; those live in YAML (or, for `node`, are deleted outright).
  Rationale: the owner's words — "things like mail should be in database for easy searching, things that could scale, like tens of thousands of mail, should remain in database" — combined with "if something can be accomplished via yaml config, like configuring what domains, what alias, where to forward, do that instead of putting them in database".
  Supersedes: an earlier decision in this conversation to eliminate the database entirely in favour of a filesystem spool. That earlier answer also implied filesystem storage for DMARC reports and daily JSON usage rollups; both are withdrawn — reports and usage stay in Postgres tables as they are today.
  Date/Author: 2026-08-18, Ziyan Zhou.

- Decision: Mail templates and layouts stay in Postgres.
  Rationale: they are user-authored content edited from the UI that grows over time, not deployment configuration. Keeping them in the database leaves the existing CRUD mutations and the pongo2 rendering path in `internal/mailer` essentially untouched.
  Date/Author: 2026-08-18, Ziyan Zhou.

- Decision: Database migrations restart from `0000_initial.sql`, describing the new reduced schema, and every migration must ship a matching `.reverse.sql`.
  Rationale: the owner explicitly authorised discarding the existing migration chain. The existing migration runner in `db/database_migrate.go` already *requires* reverse SQL — it reverts unknown migrations recorded in the `migration` table and panics if reverse SQL is missing — so the invariant is pre-existing and must be preserved. Restarting at `0000` means the owner's production database must be migrated by hand once; see `Idempotence and Recovery`.
  Date/Author: 2026-08-18, Ziyan Zhou.

- Decision: Certificates are obtained by the built-in ACME client using HTTP-01 by default, with TLS-ALPN-01 available, and the existing Route53 DNS-01 solver retained as an optional provider.
  Rationale: DNS-01 against a hardcoded hosted zone is unusable by anyone else. HTTP-01 needs only port 80, which a mail server operator already has. DNS-01 remains the only way to get a wildcard certificate, so it is kept rather than deleted.
  Date/Author: 2026-08-18, Ziyan Zhou.

- Decision: Every external cloud dependency is optional and disabled by default: S3 message archival, Route53, GeoIP, ClamAV, SpamAssassin, and the outbound SOCKS5 proxy.
  Rationale: the owner's instruction — "should make s3 usage optional, any external cloud deps should be optional". ClamAV and SpamAssassin were already made optional in commit `635815e`; the same treatment is extended to the rest.
  Date/Author: 2026-08-18, Ziyan Zhou.

- Decision: The GraphQL API is retained, including mutations, and remains generated by the reflection-based `util/graphapi` package.
  Rationale: the owner requires that "user should be able to configure everything from the web ui". Mutations that previously wrote configuration rows now write the config store, which persists to `teanode.yaml`.
  Date/Author: 2026-08-18, Ziyan Zhou.

- Decision: Multi-tenant user accounts, per-user domain ownership, and magic-link login tokens are removed. Dashboard access is a list of operators in `teanode.yaml` with bcrypt password hashes, a login form, and a signed session cookie. An empty list means the dashboard is unauthenticated, which is only sane when bound to `127.0.0.1`; the server logs a warning at startup in that case.
  Rationale: a self-hosted mail server has one operator, not tenants. Magic-link login is a bad failure mode on a fresh install because it requires working outbound mail before you can reach the dashboard.
  Date/Author: 2026-08-18, Ziyan Zhou.

- Decision: `internal/node` (Redis-backed inter-backend websocket relay), `api/node_proxy.go`, `api/node_websocket.go`, the `node` model and table, and the Redis client are deleted.
  Rationale: nothing in a single-server mail forwarder uses them, and Redis was the only remaining mandatory service besides Postgres.
  Date/Author: 2026-08-18, Ziyan Zhou.

- Decision: The frontend is rewritten as a small dashboard rather than ported. It keeps React, TypeScript, webpack and Apollo (GraphQL is staying) and drops the marketing site — `pages/articlePages.tsx`, `pages/welcomePages.tsx`, the `public/data/article/` content and the `public/media/cover/` imagery — along with `react-remarkable`, `js-yaml` and the article tooling.
  Rationale: an open-source self-hosted dashboard has no use for the hosted service's marketing pages. Flagged for the owner: this is the one item on the original drop list not individually confirmed; say the word and the article renderer stays.
  Date/Author: 2026-08-18, Claude (assumption, stated for correction).

- Decision: Domain DNS verification becomes advisory rather than a gate on accepting mail.
  Rationale: today `mx/exchange_incoming.go` rejects mail with `ErrMailBoxNotActivated` unless `domain.VerifiedAt` and the record-verified booleans are set, because in a hosted service a user can add a domain they do not own. A self-hoster owns every domain they write into their own config file; refusing their mail because a DNS check has not run yet would be a bad first-boot experience. The DNS checker still runs periodically, and the dashboard shows, per domain, exactly which records are missing and what to create. Verification state is held in memory and recomputed on start; it is derived data and does not belong in the database.
  Date/Author: 2026-08-18, Claude (design call within the owner's "keep existing functionality" instruction — the functionality is kept, its role changes from gate to advisory).

- Decision: Published history is a single fresh initial commit on an orphan branch.
  Rationale: 448 commits and 57MB of history describe an architecture that will no longer exist, and contain deployment-specific defaults. The existing history remains in the owner's private remote.
  Date/Author: 2026-08-18, Ziyan Zhou.

- Decision: The bootstrap commands (`config init`, `dkim generate`, `credential add`) must work on a configuration whose referenced files do not exist yet, so validation is split into `Validate` (structure, no filesystem access) and `ValidateFiles` (the files named actually exist). `run` and `config validate` call both; `Store.Update` calls only `Validate`, so that a dashboard edit is never rejected because of an unrelated missing file.
  Rationale: the first attempt had `dkim generate` fail to load the configuration because validation demanded the very key the command was about to create.
  Date/Author: 2026-08-18, Claude.

- Decision: Project infrastructure mirrors the owner's other repository: `mulint` and `golangci-lint` for lint, GitHub Actions for CI and releases, a `CHANGELOG.md` with a PR changelog guard, an ADR directory, and agent-facing documentation.
  Rationale: the owner asked for "release bot, mulint, ai-first coding design and adr system". The concrete interpretation of "ai-first coding design and adr system" is written out in Milestone 10; it is an interpretation and is flagged there for correction.
  Date/Author: 2026-08-18, Ziyan Zhou (request), Claude (interpretation).

## Outcomes & Retrospective

Not started. To be filled in at the end of each milestone and at completion.

## Context and Orientation

This section describes the repository as it exists before any of the work below. Read it as if you have never opened this codebase.

### What the program does

TeaNode receives email over SMTP for domains it manages, checks that the mail is authentic, and forwards it somewhere else — to another email address, to an HTTP webhook, or to another mail server. It also accepts authenticated outbound mail from the operator's own devices and relays it to the internet. It is a *forwarder and relay*, not a mailbox server: there is no IMAP and no long-term inbox.

Terms used throughout, defined in plain language:

- **MX / mail exchange**: the part of the program that decides what to do with a received message. Lives in `backend/mx/`.
- **Envelope**: the SMTP-level metadata of a message — who is sending, who it is for, the client's IP — as distinct from the `From:` and `To:` headers inside the message. Represented by `mailparse.Envelope` in `backend/util/mailparse`.
- **Alias**: a rule that matches the local part of a recipient address (the bit before the `@`) with a regular expression and says where matching mail should go. `models.Alias`.
- **Delivery**: one attempt-tracked outbound copy of a received message to one destination. A message to a catch-all plus a specific alias produces two deliveries. `models.Delivery`.
- **SPF, DKIM, DMARC, ARC**: standard email authentication mechanisms. SPF checks whether the sending IP is allowed to send for the sender's domain; DKIM checks a cryptographic signature over the message; DMARC ties those together with a policy published by the domain owner; ARC preserves earlier authentication results across forwarding hops. Implemented in `backend/util/{spf,dkim,dmarc,arc}`.
- **DSN**: Delivery Status Notification, the bounce message sent back when delivery fails. `backend/util/dsn`, `backend/mx/exchange_bounce.go`.
- **RUA / RUF**: the aggregate and forensic DMARC report addresses a domain publishes. TeaNode receives reports at signed addresses and parses them. `backend/mx/exchange_dmarc.go`.
- **ACME**: the protocol Let's Encrypt uses to issue certificates automatically. `backend/util/autoacme` is a hand-written ACME client — it is not `certmagic` or `autocert`.
- **Challenge (HTTP-01, TLS-ALPN-01, DNS-01)**: the three ways ACME proves you control a hostname — by serving a file over plain HTTP on port 80, by answering a special TLS handshake on port 443, or by publishing a DNS TXT record. Only DNS-01 is implemented today, and only against AWS Route53.
- **ULID**: a 26-character sortable identifier used as the primary key of every row. `backend/util/security.NewULID()`.

### Current repository layout

    backend/                     Go module github.com/ziyan/teanode/backend
      cmd/run/                   main(), urfave/cli v1 app, ~180 flags-worth of configuration
        main.go cli.go run.go flags.go
      api/                       GraphQL + a little REST, 3.8k lines
        api.go graphql.go graphql_schema.go
        graphql_{alias,credential,delivery,domain,layout,mail,node,report,template,token,user}.go
        graphql_websocket.go node_proxy.go node_websocket.go send.go context.go
      db/                        GORM/Postgres persistence, 4.4k lines
        db.go database.go database_migrate.go database_utils.go
        database_{user,token,domain,alias,credential,mail,delivery,report,layout,template,node}.go
        database_{domain,alias,credential}_usage.go
        migrations/              0000_initial + four later migrations, each with .reverse.sql
        dbtest/                  test harness that talks to a Postgres container
      dns/                       periodic DNS record verification for managed domains
      mailer/                    renders templates and sends transactional mail
      models/                    plain structs shared by db, api and mx
      mx/                        the mail path, 2.6k lines
        exchange.go              wiring, periodic deliver/scavenge/usage loops
        exchange_incoming.go     inbound: authenticate, store, match aliases, create deliveries
        exchange_outgoing.go     submission: authenticate credential, create deliveries
        exchange_delivery.go     attempt a delivery, retry with backoff, DSN on failure
        exchange_bounce.go       parse DSNs arriving at signed dsn+ addresses
        exchange_dmarc.go        parse DMARC aggregate reports arriving at rua+ addresses
        exchange_usage.go        in-memory usage counters flushed to Postgres every 5s
        exchange_s3.go           upload/download raw .eml to S3
        exchange_utils.go        header formatting, the parallel authenticator, alias matching
      node/                      Redis pub/sub websocket relay between backends (to be deleted)
      util/                      30 packages: protocol implementations and helpers
      web/                       gorilla/mux server, middlewares, no static serving
      version/
      vendor/                    vendored dependencies (go mod vendor)
    frontend/                    React 18 + TypeScript + webpack + Apollo + MUI
      components/ pages/ graphql/ css/ icons/ translations/ public/
    .github/workflows/
    CLAUDE.md FAQ.md FEATURES.md NOTES.md README.md TODO.md

### How a received message flows today

1. `util/smtpd` accepts the TCP connection, speaks SMTP, optionally upgrades to TLS, optionally authenticates `AUTH PLAIN` by decoding the username/password pair back into a credential id and key (`util/security.DecodeCredential`), enforces size and recipient limits, and produces a `mailparse.Envelope`.
2. `mx.exchange.HandleEnvelope` decides between four paths by looking at the envelope: a signed `dsn+`/`rua+`/`ruf+` recipient means a bounce or a report; a non-empty `CredentialID` or `DomainID` means outbound submission; otherwise it is inbound mail.
3. Inbound (`handleIncoming`): look up the recipient domain in the database, reject if not verified, run SPF/DKIM/DMARC/ARC/ClamAV/SpamAssassin/content checks in parallel, prepend `Received:` and `Authentication-Results:` headers, insert a `mail` row, match every recipient's local part against that domain's aliases, insert `delivery` rows, and hand them to a goroutine.
4. Delivery (`deliver`): sign a DSN return path, add ARC headers, connect out over `util/smtpc` (optionally through a SOCKS5 proxy), and on failure schedule a retry with a fixed backoff ladder (5m, 30m, 1h, 2h, 6h, 24h, 48h) and upload the raw message to S3 so a later retry can reload the body.
5. A `periodic` loop every minute re-queries the database for deliveries whose `retry_at` has passed and re-attempts them.

Everything in step 3 that touches `domain`, `alias` or `credential` is the part that moves to configuration.

### Reference layout

The owner's other repository, `~/projects/teanode/teanode`, is the model for the new shape: `main.go` at the root wiring a `urfave/cli/v3` app, subcommands in `cmd/`, all real code under `internal/`, frontend source under `web/` with webpack output written into `internal/frontend/static/` and embedded with `go:embed`, a `Makefile` whose default target builds both, `mulint.yaml`, `.golangci.yml` with `receiver-naming` and `ST1006` disabled so that `self` receivers pass, and `.github/workflows/` containing `ci.yml`, `release.yml`, `auto-release.yml`, `major-release.yml` and `changelog-guard.yml`. That repository is *not* a dependency and nothing is copied from it wholesale; it is a layout precedent.

## Interfaces and Dependencies

### Target repository layout

    /                            Go module github.com/ziyan/teanode
      main.go                    urfave/cli/v3 app, global flags, logging setup
      cmd/                       one file per subcommand
        run.go                   teanode run      — the server
        config.go                teanode config   — init | validate | show | edit
        password.go              teanode password — bcrypt hash for dashboard users
        dkim.go                  teanode dkim     — generate a domain key + print the DNS record
        version.go               teanode version
      internal/
        api/                     GraphQL query + mutation + subscription over config and database
        config/                  typed teanode.yaml: load, validate, watch, atomic save
        db/                      Postgres: mail, delivery, report, usage, template, layout
        dns/                     advisory DNS record checking for configured domains
        frontend/                go:embed of the built dashboard + SPA handler
        mailer/                  template rendering and transactional send
        models/                  shared structs
        mx/                      the mail path
        storage/                 message body storage: filesystem, optional S3
        util/                    protocol implementations, unchanged in behaviour
        version/
        web/                     HTTP server, middlewares, session auth
      web/                       frontend source (React, TypeScript, webpack)
      docs/
        adr/                     architecture decision records
        execplans/               this file and its successors
        architecture.md configuration.md getting-started.md deployment.md
      deploy/
        teanode.service          systemd unit
        docker-compose.yml       teanode + postgres
        teanode.example.yaml
      .github/{workflows,scripts}/
      AGENTS.md CLAUDE.md CONTRIBUTING.md CHANGELOG.md README.md LICENSE
      Makefile .golangci.yml mulint.yaml .gitattributes .gitignore

### The configuration file

This is the centrepiece of the change. `internal/config` defines these types; every field is documented in `docs/configuration.md` and in the generated example file. Field names follow the repository convention: acronyms are fully capitalised when the identifier starts with a capital (`TLS`, `ID`, `URL`), lower-camel in YAML keys (`tls`, `id`, `url`).

An operator's `teanode.yaml` looks like this:

    # TeaNode configuration. This file is the single source of truth.
    # The running server rewrites this file when configuration is changed
    # from the web dashboard; hand-written comments are not preserved
    # across such a write. Send SIGHUP to reload after editing by hand.

    server:
      name: mail.example.com          # SMTP greeting and HELO name
      dataDirectory: /opt/teanode/data
      logLevel: INFO
      logDirectory: ""                # optional: write every received .eml here

    listen:
      smtpIncoming: ":25"
      smtpOutgoing: ":587"
      http: ":80"
      https: ":443"
      debug: ""                       # e.g. 127.0.0.1:16060, empty disables pprof

    tls:
      hosts: [mail.example.com]
      certificateFile: ""             # bring your own; empty means use ACME
      privateKeyFile: ""
      acme:
        enabled: true
        email: you@example.com
        directoryURL: https://acme-v02.api.letsencrypt.org/directory
        challenge: http-01            # http-01 | tls-alpn-01 | dns-01
        accountKeyFile: acme.key      # relative to server.dataDirectory
        certificateFile: teanode.crt
        privateKeyFile: teanode.key
        route53:                      # only for challenge: dns-01
          enabled: false
          zoneID: ""
          region: us-east-1
          credentialsFile: ""
          nameservers: []

    database:
      host: 127.0.0.1
      port: 5432
      user: teanode
      password: teanode
      name: teanode
      sslMode: disable
      logQueries: false

    smtp:
      trustedSenders: [google.com, outlook.com, yahoo.com]
      maxMessageSize: 70MB
      maxRecipientsIncoming: 3
      maxRecipientsOutgoing: 50
      socks5Proxy: ""                 # optional outbound proxy
      disableSend: false              # true on a development box

    dkim:
      domain: example.com
      selector: teanode1
      selectors: [teanode1, teanode2]
      privateKeyFile: teanode1.key    # relative to server.dataDirectory

    domains:
      - id: 01K...                    # generated; stable identity for stored mail
        domain: example.com
        subdomain: mail               # the CNAME that points at this server
        comment: personal mail
        spamFilterScoreThreshold: 5
        aliases:
          - id: 01K...
            pattern: "^hello$"
            comment: public address
            kind: email
            email: you@example.net
          - id: 01K...
            pattern: "^ci-.*$"
            kind: webhook
            webhook: https://example.com/hooks/mail
          - id: 01K...
            pattern: "^team$"
            kind: mailServer
            mailServer: {host: mx.internal, port: 25, username: "", password: ""}
          - id: 01K...
            pattern: "^.*$"
            kind: email
            email: catchall@example.net
        credentials:                  # SMTP AUTH identities for submission
          - id: 01K...
            key: "..."
            comment: laptop
            disabled: false

    dashboard:
      enabled: true
      users:
        - username: admin
          passwordHash: "$2a$12$..."  # teanode password
      sessionKeyFile: session.key     # generated on first run

    antivirus:   {enabled: false, host: 127.0.0.1, port: 3310}   # ClamAV
    antispam:    {enabled: false, host: 127.0.0.1, port: 783}    # SpamAssassin
    geoip:       {enabled: false, databaseFile: ""}              # MaxMind mmdb
    storage:
      s3: {enabled: false, bucket: "", region: us-east-1, credentialsFile: ""}

Two invariants that the implementation must honour:

- Every `domains[].id`, `domains[].aliases[].id` and `domains[].credentials[].id` is a ULID that is generated once and never changes. Stored `mail` and `delivery` rows reference these strings. Editing a pattern must not change an id; deleting an alias leaves historical rows pointing at a now-unknown id, and the dashboard must render that as `(deleted)` rather than failing.
- The secret used to sign DSN return paths and credential passwords lives in `server.dataDirectory/teanode.secret`, is generated with 32 bytes of crypto/rand on first run if absent, and is never written into `teanode.yaml`. Rotating it invalidates in-flight bounce addresses and all issued SMTP AUTH passwords.

### Go interfaces to exist at the end of the work

In `internal/config`:

    package config

    // Store owns the configuration file: it holds the parsed configuration in
    // memory, hands out immutable snapshots, and persists changes atomically.
    type Store interface {
        // Current returns the active configuration. The returned value must be
        // treated as read-only; mutate through Update.
        Current() *Configuration

        // Update applies a mutation under a lock and, if the mutation returns
        // nil, validates the result and rewrites the configuration file
        // atomically. On validation or write failure the in-memory
        // configuration is left untouched.
        Update(func(*Configuration) error) error

        // Reload re-reads the configuration file from disk, e.g. on SIGHUP.
        Reload() error

        // Subscribe registers a callback invoked after every successful
        // Update or Reload, used by components that cache derived state such
        // as compiled alias patterns.
        Subscribe(func(*Configuration)) (unsubscribe func())

        Close() error
    }

    func Open(filename string) (Store, error)
    func Default() *Configuration
    func (self *Configuration) Validate() error

    // Lookup helpers used by the mail path; these compile and cache the alias
    // regular expressions so that matching does not recompile per message.
    func (self *Configuration) FindDomain(name string) *Domain
    func (self *Configuration) FindDomainByID(id string) *Domain
    func (self *Configuration) FindCredential(id string) (*Domain, *Credential)
    func (self *Domain) MatchAliases(localPart string) []*Alias

In `internal/storage`:

    package storage

    // Storage keeps raw message bodies so that a delayed delivery can be
    // retried after a restart. The filesystem implementation is always
    // present; S3 is an optional mirror for operators who want off-box copies.
    type Storage interface {
        Put(ctx context.Context, id string, headers []string, body []byte) error
        Get(ctx context.Context, id string) ([]string, []byte, error)
        Delete(ctx context.Context, id string) error
        Close() error
    }

    func Open(settings *Settings) (Storage, error)

In `internal/web`:

    package web

    // Authenticator decides whether a request may see the dashboard and the
    // GraphQL API. It is satisfied by a session-cookie implementation backed
    // by config.Store, and by a permissive implementation used when no
    // dashboard users are configured.
    type Authenticator interface {
        Authenticate(*http.Request) (username string, ok bool)
        Login(response http.ResponseWriter, username, password string) error
        Logout(response http.ResponseWriter, request *http.Request)
    }

### Dependencies

Added: `gopkg.in/yaml.v3` (configuration), `golang.org/x/crypto/bcrypt` (already present transitively via `golang.org/x/crypto`), `github.com/urfave/cli/v3` (replacing v1), `github.com/fsnotify/fsnotify` only if file watching is chosen over SIGHUP — start with SIGHUP and add fsnotify only if needed.

Removed: `github.com/redis/go-redis` (with `internal/node`), `github.com/hashicorp/yamux` if present for the relay. The AWS SDK modules (`config`, `route53`, `s3`, `feature/s3/manager`) stay in `go.mod` because Route53 and S3 remain optional features, but nothing in the default code path constructs an AWS client.

Unchanged: `gorm.io/gorm`, `gorm.io/driver/postgres`, `github.com/graphql-go/graphql`, `github.com/gorilla/{mux,handlers,websocket}`, `github.com/miekg/dns`, `github.com/oklog/ulid/v2`, `github.com/op/go-logging`, `github.com/flosch/pongo2/v4`, `github.com/oschwald/maxminddb-golang`.

## Plan of Work

Eleven milestones. Each ends with a working build and a demonstrable behaviour. Commit at the end of each; the whole sequence is squashed into one commit in Milestone 11, so intermediate commit messages are working notes, not published history.

### Milestone 1 — Root module and `internal/` layout

Scope: move code, delete dead code, keep behaviour. Nothing about configuration or the database changes yet. At the end, `make build` at the repository root produces `build/teanode`, and the binary behaves exactly as `backend/build/teanode` does today except that the node relay is gone.

Work:

- `git mv backend/{api,db,dns,mailer,models,mx,util,version,web} internal/`, `git mv backend/vendor vendor`, `git mv backend/go.mod go.mod`, `git mv backend/go.sum go.sum`, `git mv frontend web`.
- Rewrite the module path from `github.com/ziyan/teanode/backend` to `github.com/ziyan/teanode`, and every import prefix from `github.com/ziyan/teanode/backend/` to `github.com/ziyan/teanode/internal/`. A single `find . -name '*.go' -not -path './vendor/*' -exec sed -i` pass handles it; `go build ./...` proves it.
- Turn `backend/cmd/run/{main,cli,run,flags}.go` into a root `main.go` plus `cmd/run.go`, keeping urfave/cli v1 for now so this milestone stays mechanical. The v3 migration happens in Milestone 2 where the flag set is replaced by the config file anyway.
- Delete `internal/node`, `internal/api/node_proxy.go`, `internal/api/node_websocket.go`, `internal/api/graphql_node.go`, `internal/db/database_node.go`, `internal/models/node.go`, `internal/util/redisclient`, and the `node` routes in `api.AddRoutes`. Remove the `redis-*` flags and the `backend-id` plumbing that only the hub used. `db.Settings.BackendID` is used elsewhere — check with `grep -rn BackendID` before removing; keep it if the mail path uses it, remove it if only the hub did.
- Delete `backend/build/` from the working tree (it holds real keys and a 56MB GeoIP file) after confirming with the owner that they have copies. It is gitignored, so this is a local-disk action, not a git action.
- Update `Makefile` to the root, dropping the `deploy`, `syncdb`, `psql` and `docs` targets that assume the owner's servers, and adding a `web` target that builds the frontend.

Acceptance: from the repository root, `make build` succeeds and `./build/teanode --help` prints the usage. `go vet ./...` is clean. `grep -rn "ziyan/teanode/backend" --include='*.go' .` returns nothing outside `vendor/`.

### Milestone 2 — `internal/config`

Scope: the typed configuration file and its store, plus the CLI that creates and validates it. The server does not use it yet.

Work:

- Write `internal/config/config.go` with the types sketched above, `internal/config/validate.go`, `internal/config/store.go` implementing `Store`, and `internal/config/save.go` doing the atomic write through the existing `internal/util/atomicfile`.
- Validation must catch the mistakes a first-time operator makes: a domain listed twice, an alias whose `pattern` is not a valid Go regular expression, `kind: email` with an empty `email`, a `passwordHash` that is not a bcrypt hash, `challenge: dns-01` with Route53 disabled, a `dataDirectory` that does not exist and cannot be created, listen addresses that collide, and `tls.hosts` empty while ACME is enabled. Each error names the YAML path (`domains[1].aliases[0].pattern`) and says what to do.
- Write `cmd/config.go`: `teanode config init` writes a commented example file with generated ids and a generated bcrypt-hashed admin password printed once to the terminal; `teanode config validate` exits non-zero with the messages above; `teanode config show` prints the effective configuration after defaults are applied.
- Write `cmd/password.go` (`teanode password` prompts twice without echo and prints a bcrypt hash) and `cmd/dkim.go` (`teanode dkim generate --selector teanode1` writes a 2048-bit RSA key into the data directory and prints the exact `TXT` record to publish).
- Migrate `main.go` to `urfave/cli/v3`, with global `--config` (default `/opt/teanode/teanode.yaml`, env `TEANODE_CONFIG`) and `--log-level`.
- Tests: `internal/config/config_test.go` round-trips a full configuration through save and load and asserts equality; a table test asserts each validation error fires on a crafted bad file; a test asserts that a failed `Update` leaves both the in-memory configuration and the file byte-identical.

Acceptance: `teanode config init --output /tmp/t.yaml` produces a file; `teanode config validate --config /tmp/t.yaml` prints `configuration is valid`; corrupting `pattern` to `^[` makes it print `domains[0].aliases[0].pattern: invalid regular expression: missing closing ]` and exit 1.

### Milestone 3 — Simplify the database

Scope: remove configuration tables, restart migrations, keep the high-volume tables.

Work:

- Delete `internal/db/database_{user,token,domain,alias,credential,node}.go` and the corresponding `models`. Keep `database_{mail,delivery,report,template,layout}.go` and the three `*_usage.go` files.
- Rewrite `internal/db/db.go`'s `Transaction` interface to the surviving operations.
- Delete `internal/db/migrations/*.sql` and write a fresh `0000_initial.sql` plus `0000_initial.reverse.sql`. The forward file creates `mail`, `delivery`, `report`, `domain_usage`, `alias_usage`, `credential_usage`, `template`, `layout` and their indexes; the reverse file drops them in dependency order. Derive the column list from the existing migration files and the GORM models rather than from memory, and keep column names identical so the owner's production data survives a manual `INSERT INTO ... SELECT`.
- Columns that were foreign keys into deleted tables become plain `text` columns holding config ids: `mail.domain_id`, `mail.credential_id`, `delivery.alias_id`. Drop the SQL foreign key constraints; keep the indexes.
- Remove `db.Debug()` and gate SQL logging behind `database.logQueries`.
- Update `internal/db/dbtest` and the tests that reference deleted operations.

Acceptance: `make test` spins up Postgres in Docker and passes. Against an empty database, starting the server creates exactly eight tables plus `migration`. Deliberately renaming `0000` to `0001` and restarting proves the reverse path: the runner reverts `0000` using the stored reverse SQL and applies `0001`.

### Milestone 4 — The mail path reads identity from configuration

Scope: `internal/mx`, `internal/dns`, `internal/mailer` stop querying configuration tables.

Work:

- `mx.Open` takes a `config.Store` in addition to the database. `Settings` loses `Server`, `DomainKey*`, `Domain`, `S3Bucket`, `SOCKS5Proxy`, `LogDirectory`, `DisableSendMail` — all now read from the configuration snapshot at the point of use, so that a dashboard change takes effect without a restart.
- `handleIncoming` replaces `tx.GetDomainByDomain` with `configuration.FindDomain(recipientDomain)`, and drops the `VerifyAt`/`VerifiedAt` gate per the decision above — an unconfigured domain still yields `ErrMailBoxUnavailable`, which is the correct SMTP-level answer.
- `matchAliases` replaces `tx.MatchAliases` with `domain.MatchAliases(localPart)`, using precompiled regular expressions cached in the config snapshot and invalidated through `Subscribe`.
- `handleOutgoing` replaces `tx.ModifyCredential` with `configuration.FindCredential(envelope.CredentialID)`; the credential's last-used timestamp, which was a database write, becomes a usage counter row instead.
- `internal/dns` iterates the configured domains rather than querying, and publishes its findings into an in-memory `dns.Status` map keyed by domain id that the API reads. Its "email the owner when a domain breaks" behaviour becomes a log line plus dashboard state; keep the mailer notification but address it to the dashboard users' addresses if any are configured.
- `exchange_s3.go` becomes `internal/storage`, filesystem-first: bodies go to `dataDirectory/spool/<id>.eml`, mirrored to S3 only when `storage.s3.enabled`. Delivery retry reads from whichever has it. Spool files are deleted when every delivery for a mail reaches a terminal state, and swept on the existing scavenge loop.

Acceptance: the loopback test. With `smtp.disableSend: true`, `swaks --to hello@example.com --server 127.0.0.1:10025` against a config whose `example.com` alias forwards to `you@example.net` produces a `mail` row, a `delivery` row with the right `alias_id`, and a spool file — with no `domain`, `alias` or `credential` table in the database.

### Milestone 5 — Certificates without a cloud account

Work:

- Add `internal/util/autoacme/http01.go`: an `http.Handler` for `/.well-known/acme-challenge/{token}` that the manager registers with the HTTP server, plus the order/challenge flow in `acme.go` selecting HTTP-01.
- Add `internal/util/autoacme/tlsalpn01.go`: a `GetCertificate` branch that answers the `acme-tls/1` ALPN handshake with a self-signed certificate carrying the challenge extension.
- Make the solver pluggable: `type Solver interface { Present(ctx, domain, token, keyAuthorization) error; CleanUp(ctx, domain, token) error }`, with `http01`, `tlsalpn01` and the existing `route53` implementations.
- Gate Route53, S3 and GeoIP construction behind their `enabled` flags; when disabled, no AWS client is created and no mmdb file is opened. `geoip.Locator` gains a null implementation returning `nil` locations.

Acceptance: against the Let's Encrypt *staging* directory and a real hostname pointed at a test box, `teanode run` obtains a certificate over HTTP-01 within a minute and `openssl s_client -connect host:443` shows it. On a box with no AWS credentials and no `geoip` file, the server starts cleanly with no warnings about missing cloud configuration.

### Milestone 6 — Dashboard authentication

Work:

- `internal/web/auth.go`: bcrypt verification against `dashboard.users`, a session cookie signed with an HMAC key from `dataDirectory/session.key` (generated on first run), `Secure` when served over HTTPS, `HttpOnly`, `SameSite=Lax`, with a configurable lifetime defaulting to 30 days.
- `POST /api/login`, `POST /api/logout`, `GET /api/session`. A middleware rejects everything under `/api/` except those three with 401 when authentication is required.
- Delete `internal/api/graphql_{user,token}.go`, `models.User`, `models.Token`, and the magic-link flow in `internal/mailer`.
- When `dashboard.users` is empty, log `dashboard authentication is disabled; bind listen.http to 127.0.0.1 or add dashboard.users` at WARNING on every start.

Acceptance: with a user configured, `curl -i https://host/api/graphql` returns 401; logging in through the form sets a cookie and the same request returns data; `teanode password` output pasted into the config lets you log in.

### Milestone 7 — GraphQL API rework

Work:

- Queries: `domains` (from config, each with live DNS status and usage), `mails` / `mail(id)` with filtering by domain, alias, status, kind and date range plus cursor pagination, `mail(id).content` returning the parsed body parts, `deliveries`, `reports`, `usage`.
- Mutations, all of which call `config.Store.Update` and therefore rewrite `teanode.yaml`: `createDomain`, `updateDomain`, `deleteDomain`, `createAlias`, `updateAlias`, `deleteAlias`, `createCredential` (returns the generated SMTP username/password exactly once), `deleteCredential`, `updateSettings` for the non-collection sections, and `createDashboardUser` / `deleteDashboardUser`. Plus the operational ones that touch the database: `retryDelivery`, `deleteMail`.
- A new resolver for reading a stored message: it loads the raw `.eml` from `internal/storage`, parses it with `internal/util/mailparse`, and returns a structure containing the headers worth showing, a text part, a sanitised HTML part, and attachment metadata (filename, content type, size, and a download URL). Sanitisation strips scripts, event handlers, `<base>`, and rewrites remote image URLs to a blocked placeholder unless the viewer clicks "load remote content" — `internal/util/mailparse` plus the already-vendored `github.com/aymerick/douceur` and `github.com/PuerkitoBio/goquery` give the parsing and CSS handling needed.
- Keep the websocket subscription transport in `graphql_websocket.go` for live updates of the mail list.

Acceptance: `createAlias` through the GraphQL endpoint adds the alias to `teanode.yaml` on disk within the same request, and mail sent to it is forwarded without restarting the process.

### Milestone 8 — The dashboard

Work:

- Rebuild `web/` as a small app: `src/` with `pages/{login,mail,mailDetail,queue,domains,reports,settings}.tsx`, Apollo client, MUI retained, React Router. Delete the article and marketing pages, the i18n translation files if unused by the remaining screens, and the `js-yaml` / `react-remarkable` dependencies.
- The mail detail page is the point of the rewrite: envelope and authentication summary at the top (SPF/DKIM/DMARC/ARC verdicts as chips, spam score, virus result), then the message rendered — HTML in a `sandbox="allow-same-origin"` iframe with `srcdoc` set to the sanitised HTML, a text tab, an attachments list, and a "view source" tab for the raw `.eml`.
- Settings pages are forms over the config mutations: domains and aliases table with inline editing, credentials with one-time password display, integrations (ClamAV, SpamAssassin, S3, GeoIP, proxy) as toggle-plus-fields, dashboard users.
- Webpack output goes to `internal/frontend/static/`; `internal/frontend/frontend.go` embeds it with `go:embed static` and serves it: exact-match static assets with long cache headers, everything else falling through to `index.html` so client-side routing works, and `/api/` never reaching it.
- `make web` builds the frontend, `make build` depends on it, and a committed placeholder `internal/frontend/static/.gitkeep` keeps `go:embed` from failing on a clean checkout — with a build tag or a generated stub file so `go build ./...` works before `npm run build` has ever run.

Acceptance: `make` produces one binary; running it with no web server in front, visiting `https://host/`, logging in, and clicking a received message shows the message rendered with images blocked and a working "load remote content" button.

### Milestone 9 — Deployment

Work:

- `deploy/teanode.service`: systemd unit running as a dedicated `teanode` user, `AmbientCapabilities=CAP_NET_BIND_SERVICE` so ports 25/80/443/587 work without root, `Restart=always`, `ExecReload=/bin/kill -HUP $MAINPID`, hardening directives (`ProtectSystem=strict`, `ReadWritePaths=/opt/teanode`, `NoNewPrivileges`, `PrivateTmp`).
- `deploy/docker-compose.yml`: teanode plus Postgres, with the data directory and config bind-mounted, host networking for the SMTP ports.
- `docs/getting-started.md`: the ten-minute path — DNS records to create (MX, the `mail` CNAME, SPF, DKIM from `teanode dkim generate`, DMARC), install, configure, verify. `docs/deployment.md`: systemd and docker, upgrades, backups (`pg_dump` plus the data directory), and the port-25 reality check that most consumer ISPs and several cloud providers block outbound 25.
- `docs/configuration.md`: every field, its default, and what breaks if it is wrong.
- `README.md`: what it is, what it is not (no IMAP, no mailboxes), a screenshot, the quick start, and the licence.
- Choose and add a `LICENSE`. Recommend MIT unless the owner prefers otherwise — this needs the owner's decision before publication.

Acceptance: on a clean VM, following `docs/getting-started.md` verbatim produces a server that forwards a real message from an external mailbox to a real destination.

### Milestone 10 — Project infrastructure

This is the interpretation of "release bot, mulint, ai-first coding design and adr system"; correct me where it misses.

- `mulint.yaml` at the root, all analyzers on, mirroring the reference repository's configuration with this project's domain compounds (`dmarc`, `dkim`, `mailparse`, `catchall`, `webhook`, `nameserver`, `subdomain`) and acronyms (`ARC`, `DKIM`, `DMARC`, `DSN`, `EHLO`, `HELO`, `MTA`, `MX`, `RUA`, `RUF`, `SPF`, `SMTP`, `TLS`, `ULID`). `make lint` runs `golangci-lint run` then `mulint ./...` if it is on `PATH`, skipping with a notice otherwise — mulint's install source is not public as far as this plan knows, so CI treats it as optional. **Open question for the owner: where should CI install mulint from?**
- `.golangci.yml` v2 format with `receiver-naming` and `ST1006` disabled so `self` receivers pass, and `vendor/` excluded.
- `.github/workflows/ci.yml`: build, `go vet`, `make lint`, `make test` against a Postgres service container, and `npm ci && npm run build && npm run lint` for the frontend, on push and pull request.
- Release automation: `.github/workflows/release.yml` builds `linux/amd64` and `linux/arm64` static binaries on a `v*` tag, attaches them plus checksums to a GitHub release, and publishes a container image to GHCR. `.github/workflows/auto-release.yml` cuts a patch tag when commits land on `master` with changelog entries. `.github/workflows/changelog-guard.yml` fails a pull request that changes Go or TypeScript without touching `CHANGELOG.md`. `.github/scripts/{release,changelog,check-pr-changelog}.sh` hold the logic so it is runnable locally.
- `CHANGELOG.md` in Keep a Changelog format starting at `0.1.0`.
- ADR system: `docs/adr/README.md` explaining the rule (any decision that would make a future reader ask "why is it like this?" gets an ADR; ADRs are immutable once merged and are superseded rather than edited), `docs/adr/0000-template.md`, and the first records extracted from the Decision Log above — `0001-configuration-in-yaml`, `0002-postgres-for-high-volume-data-only`, `0003-single-binary-with-embedded-frontend`, `0004-acme-http-01-by-default`, `0005-drop-the-redis-node-relay`, `0006-dns-verification-is-advisory`.
- Agent-facing documentation, which is what "ai-first coding design" is taken to mean: `AGENTS.md` at the root as the single entry point for a coding agent — build and test commands, the naming conventions from the owner's global rules (acronym casing, no abbreviations, `self` receivers, `err`), the invariants an agent must not break (reverse SQL for every migration, ids are stable, the config file is the source of truth, no cloud dependency in a default code path), and where to look for what. `CLAUDE.md` becomes a short pointer to `AGENTS.md` plus Claude-specific notes. `PLANS.md` at the root records the ExecPlan discipline so `docs/execplans/` is self-explanatory. `.claude/skills/` gets repo workflows: cutting a release, adding a migration, adding an ADR.
- `.gitattributes` marking `vendor/**` as generated, `.gitignore` covering `/build`, `/data`, `web/node_modules`, `web/dist`, `internal/frontend/static/*` except the stub.

Acceptance: a pull request that edits a `.go` file without a changelog entry fails CI; tagging `v0.1.0` produces a release with four assets; `make lint` passes.

### Milestone 11 — History reset and publication

Work:

- Confirm with the owner that the old history is preserved somewhere (the existing `origin`), then `git checkout --orphan open-source`, `git add -A`, one commit titled `Initial commit`, `git branch -M master`.
- Final sweep before the commit, run by `make check-secrets` (see below): no AWS access key id, no AWS account number, no hosted zone id, no PEM private key block, no reference to the hosted service's own domain or its mail hosts, and no personal email address may appear in any tracked file. Only intentional occurrences remain: the author's name in `LICENSE` and `CHANGELOG.md`, and `example.com` throughout the documentation. Separately, rotate the forwarder key and any DKIM key that ever appeared as a flag default.
- Delete `FAQ.md`, `FEATURES.md`, `NOTES.md` and `TODO.md` or fold their still-true content into `docs/` and GitHub issues.

Acceptance: `git log --oneline` shows one commit; `du -sh .git` is a few megabytes; a fresh `git clone` of the result builds and runs.

## Concrete Steps

All commands run from the repository root unless stated.

Milestone 1:

    git checkout -b restructure
    git mv backend/go.mod go.mod && git mv backend/go.sum go.sum
    for d in api db dns mailer models mx util version web; do mkdir -p internal && git mv backend/$d internal/$d; done
    git mv backend/vendor vendor
    git mv frontend web-src && git mv web-src web   # two steps: `web` is taken by internal/web only after the move above
    sed -i 's,github.com/ziyan/teanode/backend/,github.com/ziyan/teanode/internal/,g' $(find . -name '*.go' -not -path './vendor/*')
    sed -i 's,^module github.com/ziyan/teanode/backend,module github.com/ziyan/teanode,' go.mod
    go build ./... && go vet ./...

Expected: no output from `go build`, and `go vet` clean. If `go vet` complains about the deleted node package, finish the deletions listed in Milestone 1 first.

Milestone 3, running the tests that need Postgres:

    make test

Expected tail:

    DONE 143 tests in 41.2s
    total:  (statements)  61.4%

Milestone 4, the loopback acceptance test, in three terminals:

    # 1: postgres
    docker run --rm --name teanode-postgres -e POSTGRES_DB=teanode -e POSTGRES_USER=teanode \
      -e POSTGRES_PASSWORD=teanode -p 127.0.0.1:5432:5432 postgres

    # 2: the server, with a throwaway config
    ./build/teanode config init --output /tmp/teanode.yaml
    # edit /tmp/teanode.yaml: domains[0].domain: example.com, alias ^hello$ -> you@example.net,
    # smtp.disableSend: true, listen.smtpIncoming: 127.0.0.1:10025, tls.acme.enabled: false
    ./build/teanode run --config /tmp/teanode.yaml --log-level DEBUG

    # 3: send a message
    swaks --to hello@example.com --from someone@example.net --server 127.0.0.1:10025

Expected in terminal 2, among the debug output:

    mx  [DEBUG] took 412ms to authenticate incoming mail "01K..."
    mx  [DEBUG] matched alias "01K..." pattern "^hello$" for "hello@example.com"
    mx  [WARN ] sending mail is disabled, delivery "01K..." not attempted

and in psql:

    docker exec -it teanode-postgres psql -U teanode -c 'select id, sender, subject, status from mail'

## Validation and Acceptance

The system is done when all of the following are observably true.

1. A person with only the repository can build it: `git clone`, `make`, and one binary appears at `build/teanode` with the dashboard inside it. No `backend/` directory exists.
2. `teanode config init` followed by editing one domain and one alias is enough to receive and forward mail. Nothing else is required — no AWS account, no Redis, no S3 bucket, no GeoIP file, no ClamAV, no SpamAssassin.
3. `psql -c '\dt'` shows only `mail`, `delivery`, `report`, `domain_usage`, `alias_usage`, `credential_usage`, `template`, `layout`, `migration`. No configuration is in the database.
4. Creating an alias in the web dashboard adds it to `teanode.yaml` on disk, and mail to that alias is forwarded without a restart. Editing `teanode.yaml` by hand and sending `SIGHUP` has the same effect in the other direction.
5. Opening a received message in the dashboard renders it as mail — formatted HTML with remote images blocked by default, a plain-text tab, attachments listed, and raw source available — rather than a JSON dump.
6. `make test` passes, `make lint` passes, and CI runs both on every pull request.
7. `git log --oneline | wc -l` prints `1`.
8. Every migration in `internal/db/migrations/` has a `.reverse.sql`, and reverting the newest one against a live database succeeds.

## Idempotence and Recovery

Every step above is a source edit or a local command; nothing touches the owner's production servers, and the plan deliberately contains no `ssh` or `docker save | ssh` step. The `deploy` and `syncdb` Makefile targets that did are deleted in Milestone 1.

The one genuinely destructive item is Milestone 11's history reset. Guard it: do not run it until `git remote -v` shows a remote that still has the old history, and until `git log --oneline | wc -l` on that remote branch confirms 448 commits. The orphan branch is created alongside `master`, so until `git branch -M` runs, nothing is lost and the operation can be abandoned with `git checkout master && git branch -D open-source`.

The second risk is the owner's production database. Restarting migrations at `0000` means the running production schema and the new schema share table names but not migration history. Do not point the new binary at the production database. The migration path, when the owner is ready, is: `pg_dump` the old database, create a new one, let the new binary create the new schema, then `INSERT INTO mail SELECT ...` for the columns that survive, and hand-translate the `domain`, `alias` and `credential` rows into `teanode.yaml` — a `teanode config import --from-postgres` subcommand is worth writing for exactly this and is a candidate follow-up, not part of this plan.

The frontend embed can leave the tree in a state where `go build` fails because `internal/frontend/static/` is empty. The stub file described in Milestone 8 prevents this; if it is ever removed, `make web` restores the build.

## Artifacts and Notes

The survey that informed this plan, for a reader who wants the numbers without re-running it:

    non-vendored Go, lines, excluding tests
      util     10185      api       3832      db        4442
      mx        2599      models    1081      cmd        816
      node       582      dns        406      web        203
      mailer     190      version      6

    frontend: 34 .ts/.tsx files
    git: 448 commits, 57M .git, largest blob 1.3MB (a vendored .gif)

The four SMTP-facing packages that carry the protocol weight and are *not* being restructured — `util/smtpd` (674 lines), `util/smtpc`, `util/mailparse`, and the `util/{spf,dkim,dmarc,arc,authres}` family — are the reason this project is worth open-sourcing at all. They are left alone deliberately; this plan is about everything around them.

---

Change note (2026-08-18, initial version): first version of this plan, written after a survey of the repository and eight architecture decisions taken with the owner during the session recorded in the Decision Log. Two decisions from earlier in that session — eliminating the database entirely, and moving DMARC reports and usage counters to files — were superseded within the same session by the owner's instruction to keep the database for high-volume searchable data; both the superseding and the reason are recorded above so a future reader does not resurrect the withdrawn design.
