# Refactoring follows tested behavioral boundaries

- Status: proposed
- Date: 2026-09-20
- Deciders: pending review

## Context

Mail acceptance, database transactions, background jobs and interactive agent
conversations now cross many feature packages. A current-tree review found
correctness and verification gaps that should be addressed before broad code
moves. File size alone does not identify a safe boundary.

## Proposed decision

Refactor incrementally around cancellable transactions, durable submission
outcomes, independently claimed jobs and shared application commands. Establish
regression tests first, preserve external contracts, then extract orchestration
from protocol and presentation adapters. Keep the existing infrastructure and
settled permission, configuration and deployment decisions.

The evidence is in [the review](../reviews/main-refactoring-review.md). The
implementation sequence, compatibility constraints and validation are in
[the ExecPlan](../planning/main-refactoring-execplan.md). This proposal does not
approve every future schema or behavior change; each milestone must validate
and record its concrete design before implementation proceeds.

## Consequences

The work takes several independently reviewable changes rather than one rewrite.
Cancellation may expose cleanup paths that previously outlived their callers.
Submission deduplication and claim identities need additive migrations and
coordinated worker rollout. Dashboard extraction needs asynchronous behavior
tests. Existing operation names, identifiers, secrets, permissions and protocol
contracts remain stable unless a separately reviewed decision changes them.


## HTTP query transaction scope

Ordinary HTTP GraphQL queries use a transaction per root resolver, with current
account state and permissions resolved in that transaction. Returned objects
are materialized before it ends; nested object fields only serialize those
objects. Query aliases, fragments and directives remain the GraphQL executor's
responsibility. Root wrappers are installed when the schema is built, before
requests can use it. Mutations retain their existing request transaction, and
subscription execution retains its current ownership.

Recall evaluation owns short authorization/read phases and performs embedding
calls between them. It checks the account, permission and active agent again
before returning recalled content. A model request already in flight may finish
after revocation, but its recalled content is not returned to the revoked caller.
The caller-transaction argument is removed from RecallForQuestion so it no longer
suggests that evaluation can share a transaction across model work.

A query's root fields therefore do not share a transaction or atomic failure
boundary. A transaction failure is reported on the affected GraphQL field and
other root fields can resolve independently. Query fields that perform setup
writes keep those writes within their own root transaction. This does not
promise a request-wide database snapshot; the prior read-committed transaction
also allowed successive statements to observe concurrent commits. The change
keeps external operation names and result shapes while bounding connection
ownership around individual resolver work.
