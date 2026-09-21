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
- [ ] Milestone 2 (in progress): SQL cancellation and its regressions pass; GraphQL preparation, command atomicity and bounded cleanup remain.
- [ ] Milestone 3: separate job deferrals from failures and strengthen claim identity.
- [ ] Milestone 4: make mail submission retries and storage guarantees explicit.
- [ ] Milestone 5: extract shared application commands from transport adapters.
- [ ] Milestone 6: separate knowledge ingestion, retrieval and model interpretation.
- [ ] Milestone 7: separate dashboard request state from presentation.
- [ ] Milestone 8: regularize resource lifecycle, complete protocol reviews and update operating documentation.

## Surprises & Discoveries


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


Implementation is underway. Milestone 1 and the SQL cancellation portion of
Milestone 2 are implemented and pass focused checks. The findings distinguish reproduced
failures, source-derived risks, existing security backlog and areas needing a
focused follow-up. Update this section after each milestone with actual behavior,
validation evidence, compatibility costs and remaining work.

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
