# Main branch review and refactoring priorities

Reviewed snapshot: `0f6156ff`, fetched from `origin/main`. This is a review of
current behavior and architecture, not a reconstruction of scrubbed history.
The implementation plan is [the ExecPlan](../planning/main-refactoring-execplan.md).
No production code was changed for this review.

## Findings

Priorities describe implementation order: P1 is correctness work before broad
refactoring, P2 is the next bounded improvement, and P3 is defensive cleanup.
“Reproduced” means a local check exercised the failure. “Source traced” means
the controlling paths were read but the complete user scenario was not run.

### R1. P1: Cancellation does not reach database transactions

**Reproduced.** `internal/db/database.go:223` stores the supplied context in
`TransactionContext`, but `begin` at line 249 calls `self.database.db.Begin()`
without `WithContext(self.ctx)`. Queries inherit that contextless transaction.
An already-cancelled context successfully created and committed a user in a
throwaway PostgreSQL database. The focused test failed with:

    TestReviewCancelledTransactionDoesNotWrite
    cancelled context still committed a user

Consequently, request cancellation and agent deadlines do not by themselves
cancel pending SQL or prevent subsequent writes. This also undermines the
assumption that a job deadline expires before its claim is released.

Attach the context at transaction creation, including the transaction restarted
by `Commit()`. Add cancellation-before-start and cancellation-during-a-lock-wait
regressions. Audit completion bookkeeping separately: a job still needs a short,
independent context to record that its work was cancelled.

### R2. P2: Deferrals exhaust the failure retry allowance

**Source traced.** `internal/db/database_agent.go:536` increments `attempts` on
every claim. `internal/agent/agent.go:498` returns deferred jobs to the queue
without undoing that increment; line 503 uses the same count to decide when an
actual error is terminal. Five successful deferrals followed by one transient
failure make a job dead on its first failure. Policy and
budget waits are normal operation, so deferred background work is exposed.

Preserve the existing attempt count as the number of claims if clients rely on
it. Add a distinct failure count and advance the retry ladder only on errors.
Keep deferral reasons and next-run times visible. Test several deferrals followed
by a retryable error, then the complete failure ladder.

### R3. P2: An older conversation read can replace a newer selection

**Source traced.** `web/src/components/agentDrawer.tsx:1342` issues a read and
unconditionally sets the conversation, messages, draft and todos when it
returns. `readConversation` at line 1391 remembers a promise only to clear a
loading reference. It does not prevent an older promise from updating state.
`switchTo` at line 2054 can start another read while the first is outstanding;
subscription refreshes are another producer of reads.

If conversation A is slow and B finishes first, A's later response selects A
again and replaces B's editor contents. Extract request coordination into a
hook, with a monotonically increasing request number and selected conversation
identity checked before any state writes. Use controllable promises to test
responses completing in reverse order, refresh during switching, and unmount.
A browser reproduction was not performed in this review.

### R4. P2: The dashboard lint command cannot load its configuration

**Reproduced.** `web/package.json` runs ESLint 10 through `npm run lint`, while
`web/.eslintrc.json` is the only configuration. The command exits with code 2:

    ESLint couldn't find an eslint.config.* file.

The dashboard CI job runs typechecking but never this command, so it does not
catch the broken check. There is also no dashboard behavior-test script.

Migrate the existing rules to a supported configuration, resolve the actual
findings, and run lint in CI. Add a small behavior-test suite for asynchronous
conversation and draft behavior before splitting the large components. Do not
mistake passing TypeScript checks for those tests.

### R5. P2: Distinct embedding model names can share an index name

**Source traced.** `internal/db/database_vector.go:156` lowercases model names,
replaces punctuation with underscores, and truncates the result. Distinct model
names can therefore produce the same index name. `EnsureVectorIndex` at line
127 uses `CREATE INDEX IF NOT EXISTS`, but each index has a predicate for one
exact model name. A colliding second model silently keeps the first model's
index and receives no index of its own. Correct rows may still be returned,
but large knowledge searches lose the intended indexed path.

Use a readable prefix plus a stable digest of the complete table, model and
dimension identity. Check existing index definitions before treating them as
usable. Add PostgreSQL integration tests for punctuation, case and truncation
collisions, including an existing legacy index. Do not drop working indexes
until their replacements are confirmed ready.

### R6. P2: CI omits the vector-enabled database path

**Confirmed by workflow inspection.** `.github/workflows/ci.yml` provides only
`postgres:17`. The normal local `make test` uses `pgvector/pgvector:pg17`.
`internal/db/database_vector_test.go:22` deliberately supports both paths and
can assert which path ran through `TEANODE_TEST_VECTOR`, but CI does not set it.
The ADR promises both implementations, and production compose defaults to the
extension-enabled one.

Keep the full stock PostgreSQL suite and add an extension-enabled job at least
for database and knowledge integration tests. Set `TEANODE_TEST_VECTOR=off` or
`on` explicitly so a silently unavailable extension cannot produce a green
result for the wrong implementation.

### R7. P3: One-character storage identifiers panic

**Reproduced for message storage; identical file-storage code inspected.**
`internal/storage/filesystem.go:97` and `internal/storage/files.go:49` reject
empty identifiers and path separators, then slice the final two bytes without
checking length. Calling `Put` with a one-character identifier panics instead
of returning an invalid-identifier error. Generated mail identifiers are longer;
this review did not establish an externally reachable exploit.

Share identifier validation between message and file storage and cover lengths
zero, one and two, separators, normal generated identifiers, and all read/write/
delete entry points. Keep the object-store key rules consistent with the local
backend. This is a small fix, not justification for rewriting storage.

## Design problems requiring explicit decisions

### Transaction ownership and GraphQL failure behavior

`internal/api/v1api/apigraph/graph.go:104` wraps parsing, validation and execution
in one transaction and returns nil even when GraphQL reports resolver errors.
`agentOperations.Execute` in `agent_ask.go:310` rolls back on GraphQL errors.
Meanwhile `db.Transaction.Commit()` commits and opens another transaction, and
mail and domain paths use that escape hatch. A callback therefore does not
necessarily describe one atomic operation.

The existing security review already records the missing GraphQL complexity
boundary; it is not a new finding here. Move parse/validation and resource
checks before acquiring a transaction. Define atomicity per application command,
including rollback of partially completed writes within a failing command.
GraphQL documents may legitimately have partial results, so do not claim that
all fields in a document must be atomic without a compatibility decision.
Make HTTP and agent execution share command semantics, permission resolution,
and audit attribution. Preserve separate transaction boundaries where mail
acceptance already depends on them until failure-injection tests establish the
replacement behavior.

### Sending and recording the outcome are separate operations

`SendMailboxMessage` in `internal/api/v1api/apigraph/mailbox_compose.go:194`
calls `mailer.Send`, then changes flags, removes a draft and looks up the sent
mail in its caller's transaction. `internal/mailer/mailer.go:338` hands the mail
to the exchange, which owns its own acceptance transaction. A bookkeeping error
can thus be returned after acceptance. Retrying the apparent failure can submit
a second message. This is a source-derived recovery risk; no SMTP failure
injection was performed here.

Introduce a persistent submission identifier and an explicit accepted outcome.
Retries of the same submission must return that outcome instead of accepting a
second envelope. Store pending local reconciliation work durably. This cannot
promise exactly-once delivery to a remote SMTP server: loss of the final SMTP
acknowledgement remains ambiguous. The UI must distinguish local acceptance
from remote delivery and uncertain transport outcomes.

### Local storage and shared storage have different durability promises

With a directory and S3 configured, `internal/storage/filesystem.go:139` and
`internal/storage/files.go:89` log an object-store write failure and return
success after the local write. With no directory they propagate that failure.
The behavior is intentional in the code, but deployment expectations must match
it: another instance cannot read a message that only exists on the receiving
instance. Document the supported modes and make the selected durability policy
explicit before changing return values. Add an object-store outage test for
each mode and a two-instance read-after-acceptance test for shared storage.

### Large modules mix orchestration, policy and representation

The largest manually maintained files include `agent.tsx` (3,520 lines),
`agentDrawer.tsx` (3,087), `mailbox.tsx` (2,397), `knowledge.tsx` (2,393),
`computer/scan.go` (2,202), `agent/graph.go` (1,990), `db/database_mailbox.go`
(1,905), `agent/ingest.go` (1,832), and `cmd/server/run.go` (1,402).
These sizes identify review targets, not defects by themselves.

Useful seams are conversation request coordination versus rendering; source
paging versus document persistence; graph candidate retrieval versus model
interpretation versus transactional writes; startup construction versus starting
and stopping resources; and protocol adapters versus mailbox commands. Avoid
creating a generic service framework or splitting every file at a line limit.

## Coverage and constraints

This was a broad architectural review with focused defect verification, not a
line-by-line audit of every feature or a fresh penetration test. The source
inventory contains roughly 217,000 non-test Go/TypeScript lines, including
catalogues and generated material. No claim of exhaustive correctness follows
from this pass. External model calls, live delivery, production data, actual
browser interactions, deployment upgrades and fuzz campaigns were not exercised.

| Area | Review depth and next obligation |
| --- | --- |
| Database and configuration | Traced transaction lifecycle, job claims, vector index construction and configuration interfaces; all 90 forward migrations have reverse files. Add cancellation, migration round trips and conflict/reload tests to the refactoring gates. |
| SMTP, mailboxes and storage | Traced inbound commits, mailbox/job enqueueing, send acceptance and local/S3 paths. Preserve bounce signing, authentication and acceptance invariants; exercise interrupted acceptance and delivery in the implementation. |
| GraphQL and agent tools | Compared HTTP and agent execution and permission adapters. Retain fresh permission checks and agent audit identity while extracting command ownership. |
| Jobs and knowledge | Traced claiming, deadlines, deferrals and source cursor persistence; read scan and ingest structure. Model long jobs and crash recovery explicitly before moving those routines. |
| Dashboard | Read conversation request coordination, inspected large pages and ran typechecking/lint. Add browser behavior coverage; visual parity remains unverified. |
| IMAP, DAV, calendar and contacts | Read selected-folder write checks and scheduling entry points; included existing tests in baseline. Carry forward the documented IMAP revocation/APPEND design questions; do dedicated protocol reviews during extraction. |
| Startup and integrations | Read resource setup and challenge routing; disabled S3 construction is guarded and ACME routing precedes redirect. Review every other optional integration as its constructor is moved. |
| LLM, MCP, browser, channels, skills, CLI, upgrade and protocol utilities | Inventory, existing design/security documents and suite coverage only, except paths named above. Require focused contract reviews before changes; this pass does not certify these surfaces. |

Several important controls should be preserved: database-backed configuration,
stable identifiers and signing secrets, mailbox and agent enqueueing in the same
transaction, optional integrations disabled by default, tool permissions inherited
from the person, and the existing migration reversal mechanism. Do not reopen
settled architectural decisions merely to make packages look uniform.

Documentation needs reconciliation too. `apigraph/apigraph.go` still describes
YAML-backed mutations. `docs/subsystems/jobs-and-schedules.md` says housekeeping
stops when all slots are occupied, while `agent.go:374` now runs housekeeping
before checking slots, and its timeout description omits long ingest/dream jobs.
The old security review is evidence of prior work, not proof that every current
path is covered. Preserve immutable ADRs and add amendments for new decisions.

## Validation record

Validation results are recorded in the ExecPlan's Artifacts and Notes section.
Focused probes used temporary Go overlays, so they did not modify production
code or leave failing tests in the tree. Raw logs containing local paths remain
outside the repository. Reproduced failures above are intentionally not described
as passing tests.
