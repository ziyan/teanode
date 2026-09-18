# pgvector is used when the database has it, and never required

- Status: accepted
- Date: 2026-09-15
- Deciders: Ziyan Zhou

## Context

`docs/decisions/20260910-embeddings-without-pgvector.md` stored embeddings
as `real[]` and ranked them in Go, because the compose file shipped the
stock PostgreSQL image and an operator's own database might not have the
extension. It said what that cost — "it scales to a personal mailbox, not
to a search engine" — and that pgvector could be adopted behind the same
interface later.

Later arrived with the agent's knowledge. A person's checkout and their
chat archive come to hundreds of thousands of chunks; ranking those in the
server means reading a gigabyte of floats per query, and the "newest few
thousand" candidate set that works for a mailbox means nothing for a code
corpus, where the useful answer is as likely to be eleven years old.

## Decision

**The compose file ships `pgvector/pgvector:pg17`.** It is the stock image
with one extension added: same major version, same data directory, so an
existing deployment changes the `image:` line and keeps its data.

**The server asks, once, at start.** If `pg_extension` has `vector`, or
`CREATE EXTENSION` succeeds, the database ranks vectors itself; otherwise
it does not, and says so in the log. An operator's own database that
refuses is not an error.

**The column stays `real[]`.** The index is an *expression* index —
`USING hnsw ((vector::vector(N)) vector_cosine_ops) WHERE model = '…'` —
so the rows are the same rows either path reads, and a migration never
depends on the extension. One partial index per model, because the width
is part of the index and two models differ; the width is remembered in
`vector_model` the first time a model writes.

**The index is built before the rows arrive.** Maintaining it on insert
costs about a tenth of a millisecond a row; building it afterwards over a
corpus wants more memory than a small server has. The compose file raises
`maintenance_work_mem` and `shared_buffers` for the same reason.

**Without the extension, knowledge search is words first and meaning
second**: the full-text query returns up to two thousand chunks and those
are re-ranked by cosine in the server. Memories and mail keep the bounded
candidate set they have, which is what they were designed for.

## Consequences

The one service a deployment needs is still PostgreSQL, now from a
different image. An operator who keeps the stock image loses paraphrase
matching over knowledge and nothing else.

Two code paths answer the same question, which is two things to keep
honest. `internal/db/database_vector.go` holds both, and the same test runs
against both images — `make test` uses the pgvector one,
`TEST_POSTGRES_IMAGE=postgres:17 make test` the other — asserting the same
rows in the same order.

A vector is an array of floats, which does not compress; every vector
column is `STORAGE EXTERNAL` so PostgreSQL does not spend the CPU finding
that out, and vectors live in tables of their own so that reading a chunk's
text does not drag its vector along.

`pg_dump` grows by the size of the vectors. The deployment guide says to
exclude the vector tables from a backup taken before an upgrade: they are
derived, and rebuilding them costs a few dollars of embedding and a night.

This supersedes the storage half of
`docs/decisions/20260910-embeddings-without-pgvector.md`. The reasoning
there — that a migration must not require an extension, and that the Go
path must keep working — is why this one is shaped the way it is.
