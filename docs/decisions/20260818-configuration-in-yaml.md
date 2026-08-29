# Configuration lives in one writable YAML file

- Status: superseded by
  [20260824-configuration-in-the-database.md](20260824-configuration-in-the-database.md)
- Date: 2026-08-18
- Deciders: Ziyan Zhou

## Context

The server was configured by 45 command line flags, several of which carried a
specific production deployment's values as their defaults: an AWS hosted zone
id, a shared secret, a service domain. Everything else an operator would want
to change — which domains are served, which aliases forward where, who may
relay mail — lived in PostgreSQL rows created through a GraphQL API, which
meant a new deployment had to bring up a database and drive an API before it
could receive a single message.

## Decision

All configuration lives in one YAML file, by default
`/opt/teanode/teanode.yaml`: server identity, listeners, TLS, database
connection, SMTP behaviour, the DKIM key, domains with their aliases and
credentials, dashboard users, and every optional integration.

The file is the single source of truth and is writable from both ends. An
operator edits it by hand and sends `SIGHUP`; the dashboard edits it through
the API and the server rewrites it atomically. `internal/config.Store` owns it,
hands out immutable snapshots, and validates before writing, so a rejected
change leaves both memory and disk untouched.

Configuration is deliberately **not** in the database. What stays in PostgreSQL
is data that grows without bound — see
`20260818-postgres-for-high-volume-data-only.md`.

## Consequences

- A new deployment is one file plus one binary. Nothing has to be created
  through an API before mail flows.
- A machine write reformats the file and does not preserve hand-written
  comments. A fixed explanatory header is re-emitted on every write, and every
  domain, alias and credential has a `comment` field that does survive.
- Domains, aliases and credentials need identifiers that are stable across
  edits, because stored mail and deliveries reference them. Each carries a
  generated ULID `id` that is written once and never changed. Deleting an alias
  leaves historical rows pointing at an unknown id, which the dashboard must
  render as deleted rather than failing on.
- Validation must be good, because a typo now breaks startup rather than one
  API call. Unknown fields are an error, and every message names the YAML path.
