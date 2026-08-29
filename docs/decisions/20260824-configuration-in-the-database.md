# Configuration lives in the database, and the environment says how to reach it

- Status: accepted
- Date: 2026-08-24
- Deciders: Ziyan Zhou
- Supersedes: [20260818-configuration-in-yaml.md](20260818-configuration-in-yaml.md)

## Context

The previous decision put everything in one writable YAML file, and it was the
right call for what it was solving: a new deployment became one file plus one
binary, with nothing to create through an API before mail flowed.

It has one property that cannot be worked around. The file is on one machine's
disk, and the server holds the whole of it in memory and rewrites it from
memory on every change. A second instance would have its own copy, would not
see a domain added on the first, and would overwrite the first's changes with
its own idea of the configuration the next time anything was edited. There is
no way to run two of these against one another, which rules out both a rolling
restart and a second mail host.

That also holds for the message spool: the raw `.eml` files were on the local
disk of whichever instance happened to receive the message, so no other
instance could show or retry it.

## Decision

Configuration lives in PostgreSQL, in tables — `domain`, `alias`,
`credential`, `operator`, `operator_token` for the lists, and `setting` for
the sections that are not lists. `internal/configdb` implements the same
`config.Store` interface the file store did, so everything above it is
unchanged.

Concurrent changes are resolved with a version number. One row,
`configuration_version`, is taken `FOR UPDATE` for the length of a write; a
write carrying a stale version is refused, and the store re-runs the caller's
mutation against the newer configuration rather than merging two documents.
Readers poll that one row every five seconds and reload only when it moves.

Two things cannot be kept there:

- **How to reach the database.** `TEANODE_DATABASE_URL`, from the environment.
- **Which instance this process is.** `TEANODE_INSTANCE_ID`, defaulting to the
  host name. Usage counters are accumulated by reading a row and writing it
  back, and are keyed by this; two instances sharing one lose each other's
  counts.

The environment also describes the server to create when the database has no
configuration yet — `TEANODE_SERVER_NAME`, `TEANODE_SERVER_DOMAIN` and
the rest. That happens once. Afterwards the database is the answer and those
variables are ignored, and the server logs the ones that disagree with what is
stored.

Raw messages go to an S3-compatible object store as well as to local disk,
which is what lets any instance read a message any other one handled. MinIO is
what the compose files use, because it needs no account anywhere.

## Consequences

- Several instances can run against one database. They agree on configuration
  within five seconds of a change, share the spool through the object store,
  and each keeps its own usage counters.
- A new deployment is now a compose file and an env file rather than a YAML
  file. `teanode config env` writes a starting point.
- `teanode config import` loads an existing `teanode.yaml` into the database,
  carrying identifiers, signing keys, the server secret and the session key
  across unchanged — anything else would break stored mail, SMTP passwords or
  logged-in sessions. `teanode config export` writes one back out, which is
  how a backup is taken and how a whole configuration is edited offline.
- The command line client, run without `--url`, reads the same environment the
  server does. From anywhere else it needs `--url` and a token, as before.
- `server.dataDirectory` has to be an absolute path. It used to resolve
  against the directory holding the file; there is no file, and a relative
  path would land wherever each process was started from.
- Every `config.Store` implementation has to invalidate the lookup index after
  running a mutation. Forgetting it in the new store made a credential created
  through the dashboard unusable, with a symptom — mail refused as "Invalid
  credentials" — a long way from the cause.
- Retention now has to sweep the object store as well as the local spool. A
  sweep driven from one instance's local files would never expire a message
  another instance handled.
