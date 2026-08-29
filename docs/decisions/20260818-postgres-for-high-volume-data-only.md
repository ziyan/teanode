# PostgreSQL keeps only data that grows without bound

- Status: accepted
- Date: 2026-08-18
- Deciders: Ziyan Zhou

## Context

The database held two very different kinds of thing: configuration that an
operator sets once and rarely changes (domains, aliases, credentials, users,
tokens), and operational data that accumulates forever (received mail, delivery
attempts, DMARC reports, usage counters).

Eliminating the database entirely was considered, replacing it with a
filesystem spool. That was rejected: a mail server accumulates tens of
thousands of messages, and the dashboard has to search and filter them.

## Decision

PostgreSQL stays, and holds only what grows: `mail`, `delivery`, `report`,
`domain_usage`, `alias_usage`, `credential_usage`, `template` and `layout`.

The `domain`, `alias`, `credential`, `user`, `token` and `node` tables are
removed. Domains, aliases and credentials move to the configuration file;
dashboard users become a list there with bcrypt hashes; the node relay is
deleted outright.

Mail templates and layouts stay in the database despite being author-edited,
because they are content that grows and is edited from the dashboard, not
deployment configuration.

Columns that were foreign keys into removed tables become plain text columns
holding configuration identifiers.

## Consequences

- PostgreSQL is the one service a deployment needs beyond the binary itself.
- Referential integrity between mail and the domain it arrived for is no longer
  enforced by the database. Reading code must tolerate an identifier whose
  configuration entry has since been deleted.
- Migrations restart at `0000` describing the reduced schema. An existing
  production database has to be migrated by hand once; the plan document
  describes the path.
