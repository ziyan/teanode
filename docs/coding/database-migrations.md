# Database migrations

Migrations live in `internal/db/migrations/` as pairs of SQL files, embedded in
the binary with `go:embed` and applied at startup by
`internal/db/database_migrate.go`.

## The rule

**Every migration ships a matching `.reverse.sql`.** This is not a nicety.

    0007_add_delivery_index.sql
    0007_add_delivery_index.reverse.sql

The loader panics at startup if either half of a pair is missing, so a forgotten
reverse file fails immediately and loudly rather than at the worst moment.

## Why the reverse file exists

The runner records the reverse SQL in the `migration` table when it applies a
migration. On startup it compares the migrations compiled into the binary
against the rows in that table. Any row it does not recognise is a migration
from a **newer** binary that is no longer present — which is exactly what a
rollback looks like — and it reverts that migration using the SQL it stored
earlier.

That means a downgrade works without the old binary having to know anything
about the newer schema. It also means the reverse SQL has to be correct at the
time the migration is written, because by the time it runs, the code that
described it is gone.

## Writing one

1. Add both files with the next number and a short descriptive slug.
2. The forward file makes the change. The reverse file undoes it exactly,
   dropping things in dependency order.
3. Both run inside a transaction, so no explicit `BEGIN`.
4. Update the corresponding GORM model in `internal/db/`.
5. Run `make test`, which applies every migration against a fresh database.

To prove the reverse path really works, rename your migration to a higher
number, start the server, and watch it revert the old one and apply the new.

## What not to do

- Do not edit a migration that has been released. Somebody's database has
  already applied it; add a new one.
- Do not put configuration in the database. Domains, aliases, credentials and
  dashboard users are configuration, not data; see
  `docs/decisions/20260818-postgres-for-high-volume-data-only.md`.
- Do not add a foreign key to a configuration identifier. `mail.domain_id` and
  `delivery.alias_id` hold ids from the configuration file, and the entry they
  point at may be deleted while the row lives on.
- Do not write a data migration that loads every row into memory. This table
  will have hundreds of thousands of messages in it.
