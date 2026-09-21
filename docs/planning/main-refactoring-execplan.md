# Refactor around reliable commands, cancellable work and recoverable state

This ExecPlan is a living document. Maintain Progress, Surprises & Discoveries,
Decision Log, and Outcomes & Retrospective as each change lands. The starting
snapshot is `0f6156ff`. The supporting evidence and scope limits are in
[the review](../reviews/main-refactoring-review.md). This document proposes the
implementation sequence; it does not claim the implementation is complete.

## Purpose / Big Picture


A person should be able to switch conversations without an older response
replacing their selection, retry a submitted message without creating another
local submission, and wait through an agent budget hold without losing its
failure retries. An operator should be able to cancel work, shut down the
server, and use either supported PostgreSQL image with tested behavior.
Refactoring should make those guarantees easier to change and verify.

Keep the two binaries, database-backed configuration, embedded dashboard and
existing protocol interfaces. Make a sequence of small changes with observable
acceptance criteria. Do not combine package moves with altered mail acceptance,
permission semantics, model behavior or schema contracts in one patch.

## Progress


- [x] Read repository conventions, personal instructions, ExecPlan requirements and relevant architecture decisions.
- [x] Create an isolated worktree from fetched main and record its snapshot.
- [x] Complete the architectural review and identify focused correctness findings.
- [x] Reproduce ignored database cancellation, invalid storage identifier panic and broken dashboard lint.
- [x] (2026-09-20) Draft milestones, compatibility constraints, rollback procedures and acceptance gates.
- [x] (2026-09-20) Record baseline checks and their limits.
- [x] (2026-09-20) Milestone 1: restore dashboard lint, add UI tests and vector CI, validate storage identifiers and make vector index names distinct.
- [ ] Milestone 2 (in progress): SQL cancellation, bounded job completion and GraphQL preparation with document and pagination-work limits pass; command atomicity and the remaining transaction audit remain.
- [x] (2026-09-20) Milestone 3: distinct failure accounting, per-claim completion, bounded shutdown recording, retry and migration regressions.
- [ ] Milestone 4 (in progress): mutation retry protection, disjoint delivery claims, storage modes, submission persistence and transactional exchange acceptance are implemented; mailer/coordinator/API integration, recovery worker and client retry identities remain.
- [ ] Milestone 5 (in progress): folder commands share authorization and rollback scopes; mailbox drafts/send, calendar, contacts, knowledge-source and rule-update commands remain.
- [ ] Milestone 6: separate knowledge ingestion, retrieval and model interpretation.
- [ ] Milestone 7 (in progress): extract conversation selection and read ownership, guard stale reads and preserve drafts on refresh; stream reducer, remaining state and presentation extraction remain.
- [ ] Milestone 8: regularize resource lifecycle, complete protocol reviews and update operating documentation.

## Surprises & Discoveries

The retry worker continued to dispatch after a storage read failed, leaving a
mail's headers and body empty. It now dispatches only successfully reloaded
messages, retaining the retry lease without consuming an attempt on a read
failure. A regression uses a local mailbox delivery to prove that no dispatch
occurs, without permitting network transport in the test.

Sent filing before local-recipient resolution made a message addressed to its
sender look already delivered to that mailbox. Transactional submission now
resolves local recipients before filing Sent, committing both together. This
ordering is covered for the sender's mailbox and another local mailbox, including
storage failure. Legacy SMTP acceptance retains its existing ordering until its
separate early-commit path is migrated.

The delivery retry scan updated candidates without locking their selection.
Concurrent workers could wait on the same rows and then both claim them. The
scan now selects a bounded batch with `FOR UPDATE SKIP LOCKED` before updating
retry times. A regression holds one worker's transaction open while another
claims a separate batch. This protects concurrent claims, not remote SMTP
acknowledgements or a worker still running after its retry lease expires.

The submission trace also found that fresh outgoing delivery rows have no retry
time and depend on immediate dispatch. Identified acceptance must persist a due
retry time before commit and dispatch through the claim path. Simply removing
the early commit from `handleOutgoing` is insufficient: domain and alias usage
counters updated in memory before the caller could commit or roll back. These
outgoing and alias counters now use callbacks after successful commit. Mailbox
delivery also invokes rules, agent and calendar hooks, and out-of-office replies.
The acceptance extraction must account for those effects and test storage failure
before wiring the coordinator to the API. No unused acceptance interface is
retained while those semantics are unresolved.

The shared API pagination helper returned an unlimited database query for omitted
pagination or `first: 0`, despite the documented page cap. Both now select the
1,000-row maximum, including the combined mail result. A dashboard document
audit also found that proposal cards sent an obsolete contact argument; the
mutation now maps its contact identifier to the existing `id` argument.

GraphQL document preparation needs a separate fragment-cycle validation pass
before the validator rules that compare expanded selections. Parser nesting and
expanded-selection limits must run before those rules as well. Subscription
handlers already open their own short lookup transactions; the websocket should
finish principal resolution before launching them, without putting the completed
transaction into their long-lived context.

Conversation refreshes restored the saved draft on every read, even while the
person was editing the current conversation. Restore only on a conversation
change. Pagination responses also need to match both the selected conversation
and the message array they were requested against, or an old page can prepend
itself to a different transcript.

The dashboard fetch wrapper retried every operation after a lost response,
including mutations that send mail or start agent work. A failed connection
cannot establish whether the server accepted the action. Mocked transport tests
reproduced two mutation attempts for one call before the fix.


`TransactionContext` currently means audit context, without SQL cancellation.
A database probe against a fresh PostgreSQL instance committed a write after
its context had already been cancelled. Changing that behavior will expose
callers that rely on doing bookkeeping after the work context expires.

A deferred job is a normal wait, but the queue's claim count also selects the
failure retry limit. Separating those concepts is more valuable than merely
splitting the worker into smaller files.

The dashboard typecheck succeeds while its lint command cannot start. CI
exercises the stock PostgreSQL path but not the optional vector extension path.
These gaps weaken the baseline for broad refactoring.

The queue documentation lags the implementation: housekeeping now runs with no
free slots, and ingest/dream deadlines differ from ordinary jobs. Do not spend
a milestone fixing behavior that is already correct.

## Decision Log

Decision: accepted submission records use `(owner_id, submission_id)` as their
key and retain the request digest, mailbox, original mail/Sent identities and
pending draft/reply/forward item changes. They do not cascade when mail is
deleted. A transaction-scoped advisory lock serializes even an identifier that
has no row yet; its hash never substitutes for the composite database key.
Failed acceptance rolls back without retaining a placeholder. Recovery workers
lock pending records with `SKIP LOCKED`, apply mailbox changes and mark completion
in one transaction. This keeps send deduplication distinct from delivery retries
and prevents retention from making an old accepted identifier reusable.

Decision: explicit `storage.mode` values are `local` and `shared`; an empty mode
preserves existing configurations. Shared mode requires enabled S3 and an empty
local directory. It delegates writes directly to the object store, including
errors, and keeps no local cache. Local mode requires a directory and treats S3
as an optional best-effort mirror. Both messages and opaque files use one local
write helper with private creation permissions, file flush before rename, and
directory flush after rename. This makes the required storage behavior explicit
without silently changing existing deployments or claiming that the still-open
submission coordinator already honors its errors.

Decision: validate the application-command boundary with folder operations before
moving draft/send acceptance. A command invoked within an existing transaction
uses a savepoint on that connection; a standalone command opens a transaction.
This avoids introducing a second connection that can wait on locks already held
by the caller. Failure rolls back the command, while successful commands still
belong to the outer transaction. Preserve the existing multi-field behavior while
migrating commands; HTTP/agent document-wide failure differences remain to be
resolved after command boundaries are covered. The folder slice is a foundation,
not a replacement for the submission milestone or the remaining command scope.

Decision: charge field selections by their requested page size at each paginated
ancestor, including variables, variable defaults, aliases and repeated fragments.
Omitted or nonpositive sizes use the shared 1,000-row upper bound for estimation.
The defaults are 1,000 requested items and 200,000 weighted selections. This is a
pagination-work estimate, not a promise about resolver SQL scans or unpaginated
collections. Continue auditing those resource bounds with application commands.

Decision: keep GraphQL response formatting and operation-selection errors
compatible while preparing the syntax tree outside SQL. Apply the same helper
to HTTP, agent and websocket requests. Store document limits in the existing
configuration store under `graphql`, with defaults of 32 nesting levels, 20,000
tokens and 5,000 expanded selections. Checked-in CLI and agent documents pass
these limits. Pagination-weighted execution work is a separate remaining bound;
do not describe document limits as a result-row limit.

Decision: the conversation hook owns both the displayed conversation identity
and a synchronous selection reference. A deliberate switch changes the reference
immediately; reconnects can only refresh that selection. Only the current read
may update the displayed identity or apply its snapshot. Abort stale requests,
but also check request identity because an aborted transport may still settle.
A failed switch retains the previously displayed conversation.

Decision: automatically retry only documents starting with the GraphQL query
keyword or anonymous query selection. The dashboard sends no operation name;
the server rejects documents containing multiple operations. Other document
shapes receive no automatic retry. Durable submission identity remains required
before offering safe mutation retries.


Decision: start with correctness and verification, then extract one command at
a time. Rationale: moving code before preserving cancellation, failure and
recovery semantics makes regressions harder to isolate. Status: proposed by
this review, pending implementation review.

Decision: keep existing GraphQL operations, JSON keys, configuration identifiers,
CLI behavior and protocol responses compatible. Rationale: internal naming
cleanup is not authorization to break stored configuration or external clients.
New free identifiers follow acronym casing, descriptive names, units on measured
numbers, boolean prefixes, `err` for Go errors, and `self` for Go receivers.

Decision: application commands own transactional writes; transport adapters
supply the principal and arguments. Rationale: HTTP, CLI-through-HTTP and agent
calls should enforce the same rules without requiring the agent to serialize
internal commands through GraphQL indefinitely. Keep the current operations
adapter during migration so permission checks cannot be skipped accidentally.

Decision: job completion uses an individual claim identity, not only an instance
name, and failure accounting is distinct from claim accounting. Rationale: a
late completion must not finish a newer claim on the same process identity.
Use additive schema changes with reverse SQL and a compatibility rollout.

Decision: persistent local submission identity governs retry deduplication.
Rationale: a local database can remember acceptance but cannot guarantee exactly
one remote SMTP delivery after an ambiguous network acknowledgement. Preserve
that distinction in API and UI language.

Decision: no new infrastructure service or general repository framework is
required. Rationale: PostgreSQL, current storage interfaces, existing tool
catalogues and the current React stack already supply the necessary boundaries.

## Outcomes & Retrospective


Implementation is underway. Milestones 1 and 3 are implemented, with tested
portions of Milestones 2, 4, 5 and 7. Mail acceptance and recovery, the remaining
application commands, knowledge extraction, broader dashboard extraction and
lifecycle work remain incomplete. Passing foundation checks is not completion
of this plan. The revision notes below record the current evidence and limits.

## Context and Orientation


`internal/mx` authenticates and accepts mail, creates deliveries and files mailbox
items. `internal/storage` holds message and attachment bytes. PostgreSQL keeps
mail metadata, configuration, folders, jobs and agent knowledge through
`internal/db`. A database transaction is a group of SQL operations committed or
rolled back together; the current public `Commit` method ends that group and
starts another one inside the same callback.

`internal/api/v1api/apigraph` implements GraphQL operations for the dashboard and
CLI. `internal/agent/tools` calls an operations interface whose current
implementation executes GraphQL as the person. `internal/agent/agent.go` claims
queued work and runs handlers. A claim records which worker may finish a job;
a deferral postpones a job without an operational failure.

`internal/computer/scan.go` pages files and commit records from allowed roots on
a person's device. A cursor records where the next page begins. Agent ingest
stores documents and advances that cursor; graph and dream code interpret the
material through models and write knowledge. Preserve the decisions about whose
checkouts may be read, the share of history on every page, and the manifest
belonging to a pass.

`web/src/components/agentDrawer.tsx` currently combines rendering, conversation
selection, stream subscriptions, drafts, uploads, todos and tool approvals.
`internal/cmd/server/run.go` constructs resources and manages their shutdown.
Those are composition points worth simplifying after behavior is covered.

All commands below run from the isolated checkout root unless a command changes
into `web`. Use the Go version in `go.mod`, Node satisfying `web/package.json`,
and Docker for disposable databases. Never point these tests at production.
Use reserved example domains and synthetic fixtures only.

## Plan of Work


### Milestone 1: Establish working gates and fix bounded defects


In `web`, replace `.eslintrc.json` with `eslint.config.mjs` compatible with the
installed ESLint. Carry the existing rules over intentionally, resolve missing
plugins/config dependencies and hook-rule references, and fix findings without
broad suppressions. Add `npm run lint` to the dashboard CI job. Add a behavior
test script and narrowly scoped React test tooling using versions compatible
with the checked-in React/TypeScript versions. Commit the lockfile changes.

In `.github/workflows/ci.yml`, retain the full stock PostgreSQL race suite and
add a vector-enabled test job for `internal/db` and the agent knowledge paths.
Set `TEANODE_TEST_VECTOR` to `off` and `on` respectively. Use the existing
`TestVectorRanksTheSameEitherWay` to fail if the expected backend did not run.
Do not multiply the entire slow suite until its cost is measured.

In `internal/storage`, share identifier validation in a small private helper
before slicing shard suffixes. Test malformed identifiers through both Storage
and Files without any cloud client. In `internal/db/database_vector.go`, give
index names a digest suffix of the complete identity. Test distinct model names
that differ only by punctuation, case, or text beyond the truncation boundary.
Create replacement indexes before retiring old ones; index maintenance must be
outside ordinary mailbox transactions and separately reversible.

Acceptance: lint starts and exits successfully; typechecking and behavior tests
pass; both database paths are asserted in CI; invalid identifiers return errors;
colliding model names produce separate valid partial indexes. These fixes are
independent small changes and should land separately.

### Milestone 2: Make cancellation effective and transactions explicit


Change `internal/db/database.go` so `begin` uses
`self.database.db.WithContext(self.ctx).Begin()`. Both initial creation and the
transaction restarted by `Commit()` must inherit the supplied context. Retain
`Transaction` for explicitly background work. Test a cancelled context before
start, a cancelled row-lock wait, cancellation after a write before commit, and
cancellation across the legacy commit-and-reopen path. Verify audit attribution
still works. Use database integration tests, not network-reaching unit tests.

Inspect each `TransactionContext` caller for cleanup after cancellation. In
`internal/agent/agent.go`, give completion recording a fresh bounded context
that preserves audit information. Do not reuse an expired work deadline or an
unbounded `context.Background()` for final SQL. Test that shutdown records or
safely releases a job without hanging.

In `internal/api/v1api/apigraph/graph.go`, introduce a preparation helper that
parses and validates the GraphQL document before beginning SQL. Use the vendored
parser and execution API. Bound document nesting, expanded fields and list work;
count fragment expansion and reject cycles through normal validation. Put
operator-adjustable limits into `internal/config`, with documented defaults
chosen from existing dashboard and CLI documents plus measured headroom.
Reuse preparation for agent operations and applicable websocket entry points.
Prepared execution must not parse again inside the transaction.

Record the compatibility decision for GraphQL multi-field mutations before
changing their behavior. Define and test rollback within a single failing
application command. Do not impose document-wide atomicity on clients that
currently receive partial results. Document every remaining manual `Commit()`
call and the durable boundary it protects. Add conservative database pool
configuration only after inventorying nested transactions and persistent
LISTEN connections; otherwise a small pool can deadlock callers that hold a
transaction while opening another one.

Acceptance: cancelled SQL returns promptly and does not commit; rejected
GraphQL documents never acquire a transaction; normal checked-in queries and
login mutations still work; HTTP and agent calls have the same command failure
semantics. Run the database, API, agent, mail and scheduling tests under race
detection because all share these boundaries.

### Milestone 3: Separate scheduling, claiming and retry policy


Extract pure retry decisions into `internal/agent/job_policy.go`, leaving SQL
claiming in `internal/db/database_agent.go` and the worker loop in `agent.go`.
Use the supplied test clock consistently. Add a job failure count to the model,
database mapping and a new migration with reverse SQL. Preserve the published
attempt count and initialize failure counts conservatively because old attempts
do not distinguish failures from waits. Document that existing queued jobs
receive a fresh failure allowance rather than inventing historical failures.

Add a unique claim identifier generated for each successful claim. Completion
must match job ID, running status and that claim identifier; return whether the
update actually applied. Keep the instance name for diagnostics. Coordinate
long-job timeout and stale-claim release in one policy instead of separate
special-case constants. Continue running housekeeping when no execution slots
are free. Test an old worker finishing after a job is reclaimed on the same
instance, as well as on another instance.

A mixed-version rollout needs explicit handling: old workers cannot supply the
new claim identifier. Deploy additive columns first, drain old workers, then
enable required claim matching. Do not silently accept missing claim identity
once the new invariant is enabled. Reverse the requirement before rolling back
the schema. Preserve the open-job uniqueness constraint and transactional enqueue.

Acceptance: five deferrals followed by one failure still retry; only actual
failures exhaust the ladder; a stale completion changes no row; a full worker
still queues due work; interrupted long jobs become available again only after
the agreed timeout. No live model is needed for these tests.

### Milestone 4: Make acceptance and recovery observable


Add a narrow mail submission coordinator in `internal/mailer/submission.go` and
persistence in `internal/db/database_submission.go`. Define
`SubmissionRequest` with a stable `SubmissionID`, the acting account, mailbox
and composed message, and `SubmissionOutcome` identifying local acceptance and
stored mail. An accepted submission must be bound to its owner and request
content; reusing an identifier with different content must fail.

Connect `SendMailboxMessage` to that coordinator while keeping its current
response shape. Add an optional submission identifier for updated clients;
older clients continue working without a guarantee across independently
constructed retries. The dashboard creates and retains the identifier for one
send attempt and its retries. CLI and agent adapters retain it for retries of
the same command. Persist the submission and acceptance result at the same
database boundary as mail acceptance; a record merely written before or after
`mailer.Send` is not sufficient. This requires a focused exchange refactor,
with existing inbound behavior covered before editing.

Represent pending draft/flag reconciliation as durable work tied to the
submission. Inject failure after acceptance, before local bookkeeping, and
before the response. Retrying must return the original stored mail and repair
bookkeeping without accepting another envelope. Keep normal delivery retries
separate. Add a migration and matching reverse SQL for all persistent fields.

Define storage modes in `internal/config` and `internal/storage`: local durable
storage with optional best-effort mirroring, and shared object storage required
before acceptance. Preserve the current default; require shared durability for
a supported multi-instance deployment. Share low-level path, atomic-write and
object-store error handling without conflating message retention and attachment
lifetime. Demonstrate outage behavior with an in-process fake object store and
a two-instance integration test.

Acceptance: the same identified submission is accepted once locally despite
bookkeeping failure, the draft eventually reconciles, and another instance can
read an accepted shared-storage message. Report remote SMTP uncertainty honestly.
A failed mandatory object-store write must not produce a successful acceptance.

### Milestone 5: Extract application commands one vertical slice at a time


Start with mailbox draft/save/send using the boundary established above. Create
`internal/mailbox` for mailbox command orchestration shared by GraphQL and later
protocol adapters. Keep MIME formatting in its existing utility packages,
SMTP delivery in `internal/mx`, and persistence in `internal/db`. An application
command validates an operation as a person, changes related rows atomically,
and returns a typed outcome. It is not an HTTP handler.

Introduce narrow dependencies at the consumer, using the existing grouped
operation interfaces from `internal/db/db.go` where possible. Do not replace
the database interface wholesale. Move each resolver's validation, ownership
checks and writes together into a command; leave GraphQL argument conversion
and error rendering in the resolver. Supply a freshly resolved principal and
audit actor explicitly. The agent operations adapter delegates to the same
command and continues to recheck permissions on execution.

After mailbox commands, use the same approach for calendar event save/invite,
contacts, and knowledge source changes, each as a separate reviewed change.
Before moving rule updates, incorporate the existing security review's requirement
to assess the resulting stored rule when asking for approval, then revalidate
its version at execution. Do not reintroduce the removed shell command heuristic.

Acceptance: equivalent dashboard, CLI and agent requests either perform the same
operation or return the same authorization/validation failure; database audit
rows retain the right actor. Add tests for revoked permissions and partial writes
before retiring the resolver implementation. Keep external GraphQL and JSON
names unchanged even when internal names improve.

### Milestone 6: Separate source paging, persistence and interpretation


Split `internal/computer/scan.go` by responsibility within its existing package:
root authorization, manifest lifetime, cursor encoding, file extraction and
history allocation. Keep the device wire format stable. Add contract fixtures
for paging a synthetic repository tree, resuming a pass, changed manifests,
removed roots, and the allocation of history on every page. Build fixture
repositories inside temporary directories with invented identifiers.

Split `internal/agent/ingest.go` into source scheduling, page fetching, document
filing and embedding. Define a typed internal page result with entries,
continuation cursor and completion flag; keep compatibility parsing at the
boundary for existing persisted cursor maps. Advance the cursor only alongside
successfully recorded page effects, or ensure replayed page effects are
idempotent. Deletion of unseen documents must run only after a completed pass,
never after a partial read or device disconnect.

In `internal/agent/graph.go`, `dream.go` and `remember.go`, separate retrieval of
candidate material, model request construction, response validation and
transactional application. Leave models and ranking behavior unchanged during
extraction. Keep SQL/vector access behind the existing database interfaces.
Record source-local counts, durations, failures and queue waits without logging
message contents or model prompts by default.

Acceptance: an interrupted page replays without duplicate documents or premature
deletions; source revocation prevents further reads; identical fake model
responses produce the same knowledge writes before and after extraction.
Benchmark page cost and retrieval cost with synthetic corpora under both database
images, recording time, peak memory and query counts. Compare the same fixtures
and toolchain; do not use an arbitrary file-size target as success.

### Milestone 7: Give dashboard requests one owner


First extract `web/src/hooks/useAgentConversation.ts` from `agentDrawer.tsx`.
The hook owns selected conversation identity, request sequence, loading/error
state and cancellation. It accepts results only for the current selection and
request sequence. The subscription refresh path must use the same coordinator.
Keep draft persistence scoped by conversation, and prevent a late response from
resetting user edits. Extract the stream event reducer into a pure module with
tests for replay, duplicate events and reconnect.

Then extract render-only conversation list, message list, composer and approval
components. Follow `docs/coding/frontend-design.md` and retain existing shared
controls. In separate changes, split `pages/agent.tsx` into its existing panels,
`pages/mailbox.tsx` into list/selection/compose coordination, and knowledge pages
into search, source settings and page inspection. Do not add a global state
library merely to move local state out of a long file.

Acceptance: A and B reads resolved in either order leave B selected; a refresh
does not overwrite a newer draft or todo edit; switching and unmounting release
subscriptions; reconnect reconstructs the transcript without duplicated lines.
Run behavior tests and manually check drawer and standalone views, keyboard
navigation, approval cards, attachment uploads and narrow-screen layouts. Record
visual inspection as a separate result from automated tests.

### Milestone 8: Finish lifecycle and subsystem contracts


Extract resource constructors from `internal/cmd/server/run.go` into focused
files in the same package. Preserve the existing cleanup stack and reverse
shutdown order. Construction must not start background network activity for a
disabled feature. Make start/stop ownership explicit for storage sweepers,
workers, channels, listeners, browser sessions and connected servers. Add
failure-at-each-startup-stage and cancellation-during-shutdown tests using fakes.
Keep ACME challenge routing ahead of redirects and authentication.

Complete dedicated reviews of IMAP selected-folder permission refresh and APPEND
semantics, DAV preconditions and invitation handling, MCP reconnect/OAuth state,
skill verification, LLM cancellation/stream endings, browser/device disconnect,
channel retries and upgrade rollback. Preserve their documented policies. Add
focused regressions and command reuse where evidence warrants it, rather than
rewriting all adapters to match a new abstraction. Release signing remains a
separate trust decision requiring a signing-key/distribution design; do not
claim a structural refactor solves it.

Update subsystem docs alongside the changed behavior, including queue deadlines,
transaction semantics, storage modes and recovery procedures. Correct stale
YAML descriptions and move current navigation into the existing reference docs.
Preserve immutable ADRs; new decisions require new records with consequences.

Acceptance: every constructed resource closes after partial startup, disabled
integrations create no clients or network activity, active work terminates within
bounded shutdown, and all affected protocol suites pass. Execute deployment
smoke tests on a disposable stack and verify both binaries, SMTP acceptance,
mailbox read, dashboard login and agent-off defaults before declaring the full
refactoring finished.

## Concrete Steps


Begin each implementation change by reading the affected source and its tests
at the current main revision. Recheck the review findings if the base moved.
Keep all milestones on one implementation branch and in one draft PR, with
focused commits for each bounded fix or extraction. The user explicitly requested
one PR for the complete implementation. Complete Milestone 1's gates
before broad structural work; Milestones 3 and 4 depend on Milestone 2;
Milestone 5 follows the submission boundary; Milestone 6 follows job correctness;
Milestone 7 starts with the isolated request race fix once UI tests work.
Milestone 8 closes remaining subsystem contracts and operating documentation.

From the checkout root, the standard checks are:

    make format
    make lint
    make test
    make build
    (cd web && npm run typecheck && npm run lint)
    (cd web && npm test)

`npm test` is a deliverable of Milestone 1 and does not exist at baseline.
For focused Go changes, run the affected packages first:

    go test -mod=vendor -race -count=1 ./internal/db ./internal/storage
    go test -mod=vendor -race -count=1 ./internal/agent ./internal/api/v1api/apigraph

Those direct commands skip database cases unless `TEANODE_TEST_DATABASE_HOST`
points to a disposable test database. Prefer the wrapper when validating database
behavior; it creates and removes its container automatically:

    TEANODE_TEST_VECTOR=on make test
    TEANODE_TEST_VECTOR=off TEST_POSTGRES_IMAGE=postgres:17 make test

For each schema change, apply the migration to a fresh and an upgraded disposable
database, exercise the new behavior, apply the matching reverse SQL, and reapply.
Capture row/identifier preservation and the old binary's ability to read the
rolled-back schema. Never run reverse SQL against a live deployment as a test.

Before any commit or publication, inspect the exact diff, run `make check-secrets`,
and independently check new prose, fixtures and logs for private names, addresses,
home paths, live identifiers and incident history. Do not paste raw local logs.
If implementation changes are published, follow the user's draft-only, label and
description rules; do not publish a new exploit as an ordinary public review.

## Validation and Acceptance


Success is demonstrated by the milestone behaviors, not by moved line counts.
Keep the existing SMTP, authorization, signed-return-path, configuration identity,
migration, calendar and agent permission suites. Add tests that fail on the
specific old behavior before altering it. Use fake clocks, model responses,
object stores and transports; unit tests must not contact external services.
Database tests use the repository's disposable PostgreSQL helper.

The final acceptance run includes both supported PostgreSQL paths, race detection,
frontend lint/typechecking/behavior tests, builds and a disposable deployment
smoke test. Inspect skipped tests and verify the expected backend explicitly.
Record any blocked check as blocked, never as passed. No performance claim is
accepted without a repeatable synthetic workload and before/after measurements.

## Idempotence and Recovery


Documentation edits and the read-only review can be repeated safely. Tests create
temporary data; the wrapper removes its own database container. Never stop or
remove an unrelated development container. Generated files and build artifacts
must not be included accidentally in a patch.

Each milestone should remain independently revertible. Add adapters first, move
one caller, verify behavior, then remove the old implementation. Retain old cursor
and API decoding during mixed-version operation. A schema rollback is safe only
after new writers have stopped and the older application can interpret the
remaining data. Document fields or records a reverse migration would discard
and preserve a backup before any real deployment rollback.

Submission and claim-identity changes require drain/rollout procedures rather
than an assumption that every worker upgrades together. On an uncertain send,
inspect the persisted submission and delivery outcome rather than resetting the
identifier or blindly replaying the envelope. Retire legacy indexes only after
replacement indexes are valid and the older application no longer needs them.

## Artifacts and Notes


The baseline `make test` passed under the race detector with 1,742 tests reported,
2 skipped, in approximately 109 seconds. The skipped cases require a real Chrome
DevTools endpoint and a built embedded dashboard. The wrapper created a disposable
pgvector PostgreSQL container. Overall coverage was 39.2%; this is a baseline
measurement, not a refactoring target or proof of adequate behavior coverage.

`make lint` passed, including Go formatting, secret checks, translation catalogues,
configuration documentation, golangci-lint and the installed naming checker.
Dashboard `npm run typecheck` passed. `npm run lint` failed before inspecting code
because ESLint could not find a supported configuration. All 90 forward SQL
migrations had a matching reverse file; reverse execution was not tested here.

`TestVectorRanksTheSameEitherWay` additionally passed against a separate disposable
stock PostgreSQL container with `TEANODE_TEST_VECTOR=off`. The entire stock-image
suite was not rerun. The full extension-image suite did not explicitly set
`TEANODE_TEST_VECTOR=on`, which is one reason the plan requires that assertion
in CI. No new vulnerability-database scan, browser test, production exercise or
deployment smoke test was performed.

The temporary cancellation and storage probes failed as expected against the
reviewed code. Cancellation still committed a user; a one-character storage
identifier caused a slice-bounds panic. They were executed through Go overlays,
not tracked source changes. Their synthetic fixtures and temporary databases
were isolated from application data. Full raw logs remain outside the repository.

`make build` passed for both command-line binaries. A production dashboard
build and container image build were not run during this documentation-only review.

## Interfaces and Dependencies


Preserve `config.Store`, `storage.Storage`, `storage.Files`, `db.Database` and the
public GraphQL schema while introducing narrower consumers. Keep PostgreSQL/GORM,
the vendored GraphQL parser/executor, existing SMTP utilities and the current React
stack. Only the UI behavior test runner and its necessary test adapters require
new development dependencies in this plan.

The transaction entry point remains:

    TransactionContext(ctx context.Context, function func(Transaction) error) error

Its context must govern SQL as well as auditing. Job claim APIs gain a distinct
claim identity and completion reports whether it updated a row; failure counts
are separate from the existing claim attempt count. Define these fields in
`internal/models` and update database conversion code together.

The proposed mailbox command package takes an explicit principal and request
context, and returns a typed outcome; it must not import HTTP or GraphQL.
Submission coordination lives beside the existing mailer and calls the exchange
through an acceptance interface that can join the submission transaction.
Determine that narrow signature during Milestone 4's failure-injection work and
record it here before migrating callers. A changed signature alone is not a
substitute for the acceptance invariant.

The conversation hook owns request ordering and cleanup, while presentational
components receive values and callbacks. The source page result is typed inside
the server, while the existing device cursor format is translated at its edge.
Avoid adding interfaces used solely to mock one trivial function.

Revision note: initial plan records a current-tree review and sequences verified
correctness work before structural extraction. Implementation remains pending.

Revision note: implementation is authorized end to end in the existing worktree;
the single-PR requirement supersedes the original branch-per-change suggestion.
Chrome visual verification and repeated review/fix passes are final gates.

Implementation evidence: dashboard lint and typechecking pass, and three UI
request-ownership tests pass. Storage validation, vector index collision and SQL
cancellation regressions pass under race detection with PostgreSQL and vector
indexing explicitly enabled. `make lint`, including the local naming checker,
passes. The full extension-enabled race suite passed: 1,750 tests reported, two
environment-dependent skips, in approximately 75 seconds.

Decision: use Node 22.13 or newer on the 22.x line, or Node 24 or newer for dashboard
development. The restored ESLint already requires that baseline, the new behavior
test runner supports it, and the container/CI dashboard uses Node 22. Existing
operator binaries and runtime deployment requirements are unchanged.

Implementation evidence: job retry, stale-claim and shutdown regressions pass,
including late completion on the same instance and on another instance. Migration
0090 reverses and reapplies while retaining the queued job. The full race suite
passes with vector indexing required: 1,759 tests reported, two environment skips.
Go lint and gogolint pass. CI also exposed an npm lockfile incompatibility;
regenerating with the CI npm major version and testing `npm ci` fixed it. The
clean installation, frontend lint, typechecking and behavior tests now pass.

Decision: the claim-identity upgrade requires draining all old workers rather
than supporting mixed workers with a temporary missing-identity bypass. The
forward and reverse migrations requeue interrupted claims, and the operating
procedure is documented in the jobs subsystem. This preserves the completion
check throughout the new worker's lifetime.

Revision note: the dashboard transport no longer automatically repeats mutations
when a response is lost. Ten transport regression cases cover mutation and
unknown-operation refusal, query retry, its single-attempt limit, cancellation,
and HTTP failures. All thirteen dashboard behavior tests, typechecking and lint
pass. This is a prerequisite to Milestone 4, not its durable acceptance contract.

Revision note: the first conversation extraction moves selection, pending reads,
cancellation, loading and read failures into `web/src/hooks/useAgentConversation.ts`.
Seven hook regression tests pass, bringing the dashboard behavior suite to twenty
cases. Frontend lint and typechecking pass. The production dashboard and extension
builds pass. A Chrome audit of the built framed drawer used synthetic HTTP and
subscription responses: rapid switching retained the newest conversation, a
completion refresh retained a typed draft, and desktop and narrow screenshots
showed readable controls without horizontal overflow or JavaScript exceptions.
This focused audit does not replace the later full-server deployment smoke test
or the broader dashboard audit after all extractions.

Revision note: GraphQL parsing, document validation and operation selection now
precede database work. Regression tests exercise malformed and invalid documents
through all three entry points with no database supplied, preserve query variables
and operation names, and reject excessive nesting, token counts, fragment
expansion and cycles. Subscriptions start after principal resolution commits.
API and configuration tests pass under race detection after classifying the new
lexical token count correctly in the configuration redaction guard. The full
vector-enabled suite reports 1,771 tests with one Chrome-proxy integration skip;
the embedded dashboard test now runs after the production build. Go lint and
`gogolint` pass. List-work limits, broader query headroom measurement and command
failure semantics remain open within Milestone 2.

Revision note: pagination-work checks now run during preparation. Fifteen cases
cover literal and variable sizes, object pagination, defaults, nested pages,
fragment expansion, aliases and operation selection. Shared pagination tests
cover omitted, zero, oversized and overflowing sizes while retaining cursors
and offsets. API and configuration tests pass under race detection. An audit of
226 statically resolved dashboard documents found maximum depth 7, 222 tokens,
117 expanded selections and 50,000 weighted selections when omitted page sizes
are charged at 1,000. All fit the defaults after correcting the contact-proposal
mutation; a source-document regression test preserves that correction. Client
and agent document preparation tests also pass. The remaining transaction audit
must review cross-domain list aggregation and unpaginated collection sizes;
these estimates do not replace database limits or command-level atomicity.

Revision note: [the transaction-boundary inventory](../reviews/transaction-boundaries.md)
records all nine remaining application calls to manual SQL commit, their durable
writes and subsequent work. It also records known nested-transaction cases and
why neither global rollback nor a small connection pool is a safe substitute for
extracting application commands. The broader nested-call and listener audit
remains open.

Validation update: the full race suite also passes with stock PostgreSQL and
vector indexing explicitly disabled: 1,797 tests reported, with the Chrome-proxy
integration and extension-only index checks skipped. This verifies the database
fallback path after pagination and GraphQL work estimation changes.

Revision note: database transactions now offer nested command scopes with bounded
rollback cleanup and released savepoints. Nested scopes cannot commit the parent;
a cleanup failure prevents its commit. Regression tests cover SQL errors,
command cancellation, panic recovery and outer rollback. Folder create, update,
pin and delete now share `internal/mailbox` authorization and transactional
orchestration. `access.Principal` holds the shared identity while `api.Principal`
remains a source-compatible alias. Failure injection after recursive folder
deletion restores both descendants and mail items, with the next command still
usable, in standalone and existing-transaction modes. Permission, ownership and
built-in folder protections are tested. Draft/send and the other commands remain
open; the full-suite gate is being rerun after this database-interface change.

Validation update: the full vector-enabled race suite passes after command-scope
and folder extraction: 1,805 tests reported, with the Chrome-proxy integration
skipped. A subsequent focused regression also confirms that a folder command
reuses a row lock held by its caller instead of waiting for a second connection.
Go lint and `gogolint` pass.

Revision note: delivery retry selection now locks candidates with
`FOR UPDATE SKIP LOCKED` before postponing them. The focused database delivery
tests pass under the race detector, including two concurrent workers with the
first transaction held open. `make lint`, including `gogolint`, passes. This is
a prerequisite to durable submission dispatch; acceptance, bookkeeping recovery
and storage modes remain open.

Revision note: explicit local/shared storage modes now validate in configuration
and storage construction, and the server passes the selected mode through.
Message and file writes share private atomic file creation and file/directory
flushes. A fake object store verifies that shared writes report outages, local
writes survive mirror outages, and a second storage instance reads a successful
shared write. These tests use no network. Storage and configuration race tests
pass. Full server acceptance and two-instance submission recovery remain open;
these storage-instance tests alone do not satisfy that acceptance gate.

Revision note: CI identified an integer-conversion warning in GraphQL page-work
estimation. The conversion now explicitly enforces the signed 32-bit GraphQL
integer range independently of configuration validation, with boundary tests.

Validation update: the vector-enabled race suite reports 1,814 tests with one
skip after delivery claims, storage modes and integer conversion bounds. The
Chrome-proxy integration remains opt-in. A subsequent focused storage race run
also covers failed-rename cleanup and the existing-spool startup adjustment.
The stock PostgreSQL race suite reports 1,814 tests with two skips (Chrome proxy
and the vector-extension-specific index check). Go lint and `gogolint` pass.

Revision note: migration `0091_mail_submission` and its reverse add accepted
submission identities and pending mailbox reconciliation. Database tests cover
acceptance rollback, failed reconciliation remaining pending, identity survival
after mail deletion, owner-scoped concurrent retry locks, separate recovery
worker batches and migration reversal/reapplication. The persistence interface
is a prerequisite, not a completed send path: no adapter uses it yet.

The exchange now accepts mailbox submissions into a command savepoint in its
caller's transaction without early commit or immediate delivery. Storage errors
roll back mail, Sent, recipient items and delivery rows, even when the caller
commits its enclosing transaction. Successful external recipients receive a due
retry time before commit. In-memory outgoing and alias usage waits for the owning
transaction's commit and honors nested rollback. The coordinator
must lock/check the accepted identity before rebuilding message content or
looking up a draft that reconciliation may already have deleted. Bind the digest
to the stable server-serialized request parameters, including mailbox and
attachment references, excluding generated MIME identifiers and timestamps.
Reconciliation can run under a command savepoint after acceptance writes, with
failure leaving the durable record pending; a worker must retry after commit.
Reuse the caller's transaction so a second connection never waits on its locks.

Downgrading past `0091_mail_submission` discards all accepted retry identities
and pending reconciliation. Finish reconciliation and stop sending clients
before an intentional revert; retries of pre-downgrade requests cannot retain
their deduplication guarantee after that table is removed.

Validation update: all four submission database regressions pass under the race
detector. The full vector-enabled race suite reports 1,818 tests with the
opt-in Chrome-proxy test skipped. Go lint and `gogolint` pass.

Revision note: `Transaction.AfterCommit` now supports short, non-durable memory
updates. Root rollback, failed/cancelled commit and failed command savepoints
discard them. Successful nested commands transfer callbacks to the parent only
after their savepoint is released. Manual commit consumes a batch once before
reopening. Outgoing acceptance, local-domain handoff and alias matching use this
for usage counters; durable delivery and reconciliation continue to require SQL
queue records. Tests cover nested successes inside rolled-back commands, parent
rollback/cancellation and manual commit/reopen. The stock PostgreSQL race suite
reports 1,821 tests with two expected skips. An additional focused mail-path
regression verifies that alias counters exclude a rolled-back delivery and count
the successful retry once. Lint and `gogolint` pass.

Revision note: CI continued to flag the page-size conversion through float64
despite the explicit range guard. Integer inputs now remain integers through
their final bound check; floating-point variables receive separate finite,
integral and range validation before conversion. This also rejects negative
infinity and negative fractions instead of treating them as default page sizes.
The focused GraphQL race tests and lint pass; the next CI run must confirm the
conversion annotation is gone.

Revision note: `Exchange.AcceptSubmission` now implements that transactional
boundary. External-recipient tests cover storage failure, parent rollback and
successful commit, including saved bytes, Sent, queue eligibility and usage.
Local-recipient tests cover Inbox and Sent committing or rolling back together,
including a message addressed to its own sender. The entire mail-exchange test
package passes under the race detector. The mailer and public adapters still use
legacy sending and must be connected before this changes their acceptance path.

Next integration detail: `mailer.Compose` opens a separate lookup transaction,
and `rewriteMedia` reads media and creates tracking links directly on the root
database. The identified submission path must reuse its command transaction for
both, while preserving optional-image fallback behavior with a savepoint if a
media-link write fails. Check an accepted identity before composing, since MIME
identifiers and media tokens change on each composition. The delivery poll is
currently eight records per minute, and a failed storage reload keeps its
two-hour claim. Before routing normal sends through it, add prompt bounded
dispatch and a retry policy for storage outages that cannot overwrite a newer
claim. Submission reconciliation still needs its worker and adapter integration.

CI update: the completed CodeQL and Go analysis checks on the integer-preserving
page-size change pass. The earlier conversion annotation is gone.

Validation update: the full vector-enabled race suite reports 1,828 tests with
the opt-in Chrome-proxy test skipped after transactional exchange acceptance
and storage-read dispatch protection. Go lint and `gogolint` pass.
