# Contributing to TeaNode

TeaNode is a mail server. Mail is unforgiving: a mistake does not throw an
exception, it silently loses somebody's message or gets a domain's reputation
burned. That shapes most of what follows.

Start with `docs/reference/local-development.md` to get a build and a database.

## Before you send a change

    make format        # gofmt
    make lint          # golangci-lint, then mulint if you have it
    make test          # starts a PostgreSQL container automatically
    make build

CI runs `make lint-ci`, which is golangci-lint only. `mulint` enforces this
project's naming conventions and is local-only, so a contributor without it is
never blocked; run it anyway if you have it.

## Naming

These are not negotiable, and `mulint` checks most of them.

- **Acronyms follow the first letter.** If the identifier starts with a
  capital, acronyms are fully capitalised: `ReferenceURI`, `SessionID`,
  `GetFTPID`, `DKIMResult`. If it starts lowercase, only the first letter of
  the acronym is capitalised: `referenceUri`, `sessionId`, `getFtpId`. Register
  a new acronym in `mulint.yaml` with a comment saying what it stands for.
- **Do not abbreviate.** `command`, not `cmd`. `response`, not `resp`.
  `request`, not `req`. Go package names are the exception and should be short.
- **No single-letter variables.** `err` is the one blessed short name, and Go
  errors should be called `err` wherever possible.
- **Struct receivers are `self`.** Consistently, across the whole codebase.
  `.golangci.yml` disables the linters that would object.
- **Name the same thing the same way everywhere.** If it is a `delivery` in the
  database it is not a `send` in the API.

## Comments

Explain **why**, not what. The what is in the code underneath.

A comment earns its place when it records something the reader cannot recover
from the code: a protocol requirement, a failure that was observed in
production, a deliberate choice among several reasonable ones. `// increment
the counter` above `counter++` is noise. `// Reject before the DATA command so
a spammer pays for the connection` is worth having.

Exported identifiers get a doc comment starting with their name.

## Invariants

Break any of these and something downstream fails in a way that is hard to
trace back.

- **Every migration ships a matching `.reverse.sql`.** The migration runner in
  `internal/db/database_migrate.go` reverts unknown migrations using the
  reverse SQL it recorded when applying them, and panics when it is missing.
  See `docs/coding/database-migrations.md`.
- **Configuration identifiers are stable.** A domain, alias or credential `id`
  in the configuration is generated once and never changed, because stored mail
  and deliveries reference it. Editing a pattern must not regenerate an id.
- **The configuration file is the source of truth.** Do not add a settings
  table. Anything an operator sets belongs in `internal/config`.
- **No cloud dependency in a default code path.** S3, Route53 and GeoIP are
  optional and off. Code guarded by `if settings.Enabled` must not construct a
  client, open a file or dial anything when disabled.
- **The ACME `http-01` handler comes first.** A certificate authority fetches
  `/.well-known/acme-challenge/` over plain HTTP with no credentials. It must
  not meet authentication, a redirect to HTTPS, or a catch-all route.
- **The server secret is generated once.** It signs bounce return paths and
  every SMTP credential's password. Rotating it invalidates all of them.
- **Never commit a secret or a real address.** `make check-secrets` scans
  tracked files. Test fixtures use `example.com` and `example.net`.

## Tests

Write the test that would have caught the bug. A test that only proves the code
runs is not worth the maintenance.

Tests that need PostgreSQL take it from `internal/db/dbtest`, which skips when
`TEANODE_TEST_DATABASE_HOST` is unset, so `go test` still works without Docker.

**A unit test must not reach the network.** This was learned the hard way: an
early version of `autoacme.Open` started its renewal loop during construction,
so merely building a manager in a test contacted Let's Encrypt production.
Construction and starting are now separate for exactly this reason.

## Commits

Write the message for somebody trying to understand this change a year from
now with no memory of the conversation that produced it.

- Subject in the imperative, under about 72 characters: "Obtain certificates
  without a cloud account".
- Body explains why the change was needed and what it costs. Describing what
  the diff does is redundant; the diff is right there.
- Note behaviour that changed for an operator, and anything that has to be done
  by hand when upgrading.

## Changelog

User-visible changes get an entry in `CHANGELOG.md` under Unreleased, in the
Keep a Changelog categories. Internal refactoring that an operator cannot
observe does not need one.

## Decisions

If your change makes a choice a future reader would question, write a decision
record in `docs/decisions/` — see the README there. Larger work gets a design
document in `docs/planning/active/` first, moved to `done/` when it lands.

## Reporting a security problem

Do not open a public issue for anything exploitable in mail handling,
authentication or certificate issuance. Mail it to the address in `README.md`.
