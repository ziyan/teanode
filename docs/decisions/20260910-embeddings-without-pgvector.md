# Embeddings are stored as plain arrays and ranked in Go, without pgvector

- Status: accepted
- Date: 2026-09-10
- Deciders: Ziyan Zhou

## Context

Search by meaning needs a vector per message and a way to rank a query's
vector against them. PostgreSQL has an extension for exactly this,
`pgvector`, with indexes that make ranking fast at any scale.

This server's compose file runs the stock PostgreSQL image, and an operator
who brought their own database may not have the extension and may not be
able to install it. A migration that requires it would refuse to start on
those servers, which is the kind of surprise a self-hosted upgrade must not
spring.

## Decision

A message's embedding is a `real[]` column in an ordinary table, with the
name of the model that produced it beside it. Ranking is cosine similarity
computed in Go over a bounded candidate set: the mailbox's newest few
thousand messages. Where a search also has words, the two are separate
searches whose answers are joined, what the words hit first; they are not
intersected. A change of embedding model marks the rows stale by their model
name, so until they are written again that mailbox's search falls back to
words.

## Consequences

It scales to a personal mailbox, not to a search engine. A mailbox with a
hundred thousand messages ranks the newest few thousand, and does not say so
to the model, which is a gap: a search that finds nothing old cannot be told
apart from one that finds nothing. If a deployment ever needs more, `pgvector`
can be adopted behind the same interface, as an option the migration checks
for rather than requires.

No extension, no new image, no new operator step. The reverse migration is a
plain `DROP TABLE`.

## Since

The same decision was taken again for a person's memories, which carry their
vector and its model name on their own row and are ranked the same way, over
a thousand candidates rather than three thousand. Memories are written a few
per conversation turn, so a change of model catches up on its own. Messages
are not: they are only embedded when they arrive, or by the backfill that
runs when a mailbox is granted with sorting on, and rows from an old model
are never removed.
