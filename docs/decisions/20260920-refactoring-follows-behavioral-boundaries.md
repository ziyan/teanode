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
