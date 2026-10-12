# Passive meeting recordings, attributed transcripts and memory

This ExecPlan is a living document. Keep Progress, Surprises & Discoveries,
Decision Log, and Outcomes & Retrospective current. Design: issue #423.

## Purpose / Big Picture

A person explicitly starts a silent local recording, or imports audio/video.
Their original media survives interruptions and upload retries. They can browse,
play and correct versioned speaker-labeled transcripts before learning finishes.
Existing knowledge ingestion and dreaming learn proposals, decisions, questions
and commitments with source versions and time ranges, without an approval gate.
Recorded speech is evidence and cannot authorize a tool call.

Teams organization integration, direct call capture, wearable synchronization,
always-on activation and visual video understanding are deferred.

## Progress

- [x] Read the full issue, repository instructions and provider/voice/memory docs.
- [x] Preserve ongoing work and isolate this feature on its own branch/checkout.
- [x] Notify the other active repository session of branch ownership.
- [x] Verify current official file-transcription, reference and pricing contracts.
- [x] Increment 1 merged in PR #425, with all CI checks passing: optional Transcriber capability, registry resolution,
  normalized segments/usage and the documented OpenAI adapter with fake-provider tests.
- [x] Increment 2a implemented locally: account-owned manifests, immutable original
  parts, SHA-256 verification, durable receipts and resumable deletion.
- [x] Increment 3a implemented locally: authenticated GraphQL library/capture/media
  metadata, bounded part uploads, completion and private byte-range playback.
- [x] Durable media increment reviewed, full lint and CI passed, squash merged in PR #427.
- [x] Deploy merged durable media after confirming preservation of existing deployed work.
- [x] Increment 2b: transcript runs/versions merged in #433; correction reconciliation follows in #437.
- [x] Increment 3b: background transcription, existing budgets and usage accounting merged in #434.
- [ ] Increment 4: recording transcript knowledge source, automatic dreaming
  and corrections/deletion that reconcile dependent memories.
- [ ] Increment 5: speaker/group/passage correction and automatic voice clips.
- [ ] Increment 6: dashboard import, playback and transcript correction.
- [ ] Four-surface parity: web, CLI, LLM tools and iOS for every recording capability
  and operator setting, with equivalent account ownership and error semantics.
- [ ] Chrome visual and interaction audits at desktop and phone widths.
- [ ] Review every increment, fix findings, and verify its completed CI before merge.
- [ ] Increment 7: iOS capture, interruption recovery, background upload,
  import/share and recording Live Activity with local pause/resume.
- [ ] Increment 8: real-device and representative-meeting evaluation.

## Surprises & Discoveries

- internal/llm/provider.go already separates Service, Provider, Embedder and
  Decider. Registry.resolve enforces enabled providers and model admission.
  Transcription will be another optional capability on the same clients.
- Existing voice mode submits utterances as user turns. Passive capture must
  have separate entry points and must never reuse that submit behavior.
- storage.Files already stores arbitrary bytes on the same filesystem/S3
  storage as other files. Its current byte-slice API is bounded in memory;
  large media needs streaming/range support in this subsystem, not another store.
- Knowledge source generations already reject stale ingestion progress.
  Documents, evidence and page revisions provide the reconciliation foundation.
- iOS already declares background audio and has call/turn Live Activities,
  but call behavior is not proof that passive capture survives screen lock.
- No global ExecPlan template is installed at the instructed location; this
  document follows the repository's existing ExecPlans.
- The service migration already in review adds a database migration. Allocate
  recording migration numbers against the current shared base before integration.
- Current main includes the provider foundation and the two reviewed iOS fixes.
  The service PR has now merged after all CI checks passed. Its sync credential
  migration uses 0163; recording storage uses 0164 with matching reverse SQL.
- Streaming and byte-range access now extend the existing filesystem/S3 blob store.
  Fake S3 transport and filesystem race tests verify unchanged bytes, bounded range
  reads, canceled writes and protection of an existing original after failed writes.
- A raw media file can be represented by immutable ordered upload-part blobs. Its
  full SHA-256 and durable manifest establish the original file without assembling
  a second permanent copy. Playback and processing stream those same bytes.

## Decision Log

1. Reuse configured provider clients and registry admission. Select transcription
   explicitly by provider:model and required capabilities. No silent downgrade
   from requested diarization to plain text. Unknown model capabilities are unknown,
   even if an OpenAI-compatible server lists that model.
2. Keep model-local speaker IDs scoped to one transcription request/chunk. A label
   in another request is not identity evidence. Use confirmed named references
   where supported and preserve unresolved attribution when linking chunks.
3. Store original bytes unchanged, with owner, capture/upload identifiers, digest,
   size, content type and ordered timeline/gap metadata in PostgreSQL. Commit a
   durable receipt only after verified bytes exist. Same upload key/digest retries
   return the same receipt; conflicting bytes are refused.
4. Store each generated transcript run/version immutably. Corrections are separate,
   with recording/version/segment/speaker targets and revisions. Apply current
   corrections to display/ingestion; reprocessing does not overwrite them.
5. Name assignment changes metadata without a provider call. It automatically
   queues clean single-speaker clip curation. Clip quality includes overlap,
   duration, signal/noise and uncertainty; poor excerpts produce no reference.
   Clips retain source/version/time provenance and never use unconfirmed guesses.
6. Source changes queue ingestion and affected evidence/memory reconciliation.
   Preserve proposals versus decisions versus commitments and uncertainty in
   attributed evidence. Transcript text reaches learning as untrusted source data,
   never as instructions or a new user message.
7. Explicit deletion removes originals, derived audio, versions/corrections and
   reference clips through the existing storage lifecycle, and reconciles memories
   that cite the deleted source. Retry cleanup through an owned durable job.
8. Local iOS capture uses recoverable bounded files and an atomic manifest, not a
   continuously uploaded call. Capture survives network loss. Interruption/crash
   recovery shows actual recoverable bytes and gaps, without claiming uninterrupted
   recording while the OS suspended capture. Release files only on durable receipts.
9. Live Activity controls act on the local recording, including locked-screen
   pause/resume. No speech synthesis, fake agent call, or conversation turn.

## Provider verification and billing

Official sources checked during implementation:

- [File transcription](https://developers.openai.com/api/docs/guides/speech-to-text):
  diarized_json supplies speaker/start/end segments; use automatic chunking for
  long requests. Up to 4 named reference clips, each 2 to 10 seconds, are supported.
  Requests are limited to 25 MB, so long media needs bounded audio chunks with a
  preserved source timeline. The replacement transcription models must be tested
  for speaker/reference support before substitution.
- [Diarization model](https://developers.openai.com/api/docs/models/gpt-4o-transcribe-diarize):
  the model page shows token rates. This does not establish a per-minute invoice.
- [Pricing](https://developers.openai.com/api/docs/pricing): visible plain-model
  estimates do not verify diarization billing. Do not present the provisional
  $0.006/min estimate as an actual price.
- [Deprecations](https://developers.openai.com/api/docs/deprecations):
  the current diarization model is scheduled for shutdown February 26, 2027.

Persist provider-reported raw usage and normalized token/duration metrics with
each run, including whether metrics were reported. Estimate cost separately,
using configured prices and known duration. Unknown cost is not zero actual cost.
Record the configured model identity on usage rows. Compare a controlled paid
sample's reported usage against account billing before quoting measured cost;
no paid sample or invoice has been verified in this increment.

## Plan of Work

Increment 1 is a small provider boundary with no database migration or runtime
activation. Add transcription.go and openai_transcription.go, registry selection,
and tests for diarized multipart requests, references, timestamps, usage, refused
capabilities, malformed results, provider errors and cancellation. Construction
must not dial anything. Return plain model output only when explicitly requested.

Increment 2 adds the recording domain and reversible migrations, ownership checks,
upload identity and storage receipts. Extend existing Files with streaming/range
interfaces where needed. Model capture/import, upload, transcribe and learn states
separately. No parallel provider or queue framework.

Increment 3 connects APIs, cancellable jobs, usage and budgets. Before each paid
attempt enforce existing limits; preserve failed/unknown usage and distinguish a
retry from a free metadata change. Cancel stale versions after corrections.

Increment 4 creates one source/document per recording transcript generation and
timestamped passages. Use existing digest/dream/revision paths. A correction names
the invalidated evidence and queues affected facts for revision, preserving history.
Exercise an adversarial transcript containing deployment requests without actions.

Increment 5 curates references automatically from confirmed corrections. Choose
multiple suitable excerpts, invalidate revoked/misassigned clips, and condition
future requests only when supported. Test merged speakers and passage-only labels.
Never infer cross-chunk identity from a repeated provider label.

Increment 6 uses dashboard design conventions and a dedicated recording library,
resumable imports, playback, version selection, clear processing errors and speaker
correction. Video preserves original media and derives audio with aligned timestamps.

Increment 7 implements the native recorder independently of VoiceCall, an App Group
capture/upload manifest, background transfers, share imports and a recording-specific
Live Activity. Test state/manifest/upload logic in TeaNodeKit; compile app/widgets
on macOS CI. Validate screen lock and controls on an actual iPhone.

Increment 8 evaluates supported representative media, mixed voices, overlap,
interruptions, retries and reprocessing. Report observed speaker error, attribution
quality, useful learned facts, billable usage/cost and battery impact. Use synthetic
or explicitly supplied fixtures in the repository; do not publish private sources.

## Concrete Steps and Validation

Run changed-file gofmt, make lint, focused Go tests and the build for each increment.
Database changes require forward/reverse migration checks and PostgreSQL ownership,
idempotency, cleanup and concurrent retry tests. Unit tests use fake transports or
loopback fixtures and never contact production providers.

Test transcript correction preservation, chunk-local identity, clean-reference
selection, stale job cancellation, deletion cleanup and evidence reconciliation.
Compare original upload/import digests byte-for-byte. Exercise network loss and
backend receipt loss without deleting the local source or duplicating records.

Run dashboard typecheck/lint/behavior tests. Use Chrome to visually and interactively
audit desktop and phone widths, including empty, uploading, retry, processing,
playback, error and correction states. Exercise equivalent operations through the
LLM tool and CLI against the same backend; interfaces must share ownership,
metadata changes, explicit paid reprocessing and deletion behavior. Run Swift
package tests and macOS simulator builds. Keep real-device acceptance unchecked
until screen lock, interruptions, pause/resume, restart recovery, background uploads
and battery measurements have actually been observed.

A draft PR must link this plan and issue, remain AI-generated and draft, and describe
only the increment it contains. Update the issue with tests and unresolved checks.

## Idempotence and Recovery

Capture/session/chunk/upload/run identities are stable. Advance database state only
under owner-scoped locks and matching source/transcript generation. A duplicate
receipt does not re-enqueue paid transcription. Retain unknown-result attempts for
reconciliation instead of assuming a provider call cost nothing. Rerunning a
transcription may require a full paid pass. Metadata labels never require one.

## Current implementation checkpoint

PR #427 at 3aee8a0 contains the durable media increment. Its previous completed CI
run passed; the quota/settings and Chrome layout review fixes now pass all CI.
Work covers
account-owned recording/media manifests, bounded resumable part uploads, verified
whole-file receipts, streaming playback and durable explicit-deletion cleanup.
The service work is merged. The original working copy is preserved. Reviewed recording foundations are now deployed; unfinished integration branches are not deployed.
Draft PR #433 at d53579d contains the transcript persistence and decoder
foundation, with local validation complete and CI in progress. The next branch,
feat/recording-library, starts the provider worker and matching library interfaces.
Subsequent increments add sources/reconciliation, corrections/references, dashboard,
CLI/tool parity and native capture. The feature is not complete while any of those
surfaces or the desktop/phone Chrome audits are missing. Provider billing and actual iPhone
behavior remain open evidence requirements. The issue must remain open until those
acceptance criteria are observed.

## Processing implementation checkpoint

The transcript branch now commits immutable per-media versions, request-local
segments, provider attempt receipts and derived-blob deletion references.
PostgreSQL race tests cover ownership, stale worker claims, retry idempotence,
unknown remote outcomes, nullable actual costs and forward/reverse migrations.
Local FFmpeg tests and the packaged-reader container tests pass, including
delayed video audio, cancellation, duration bounds and refusal of external
playlists. The container reader tests run without networking and cannot skip.
The production image retains its nonroot, shellless runtime. Provider execution,
usage integration, corrected identities, library interfaces and native capture
remain unimplemented; this is a processing foundation, not a complete feature.

The first CI run found the new job kind missing from run labels. Web catalogs,
web/iOS filters and the native label now include recording transcription.

The shared client documents and transport now pass schema, receipt-integrity and
read-only connection tests. CLI/tool commands and dashboard flows still need to
be wired to these operations. Both implementation PRs remain draft for author review.

## Processing implementation design

Use the existing agent queue for preparation and transcription. A completed media
manifest admits one initial version; explicit reprocessing creates another version.
Automatic retries reuse committed chunks and completed attempts. Before a provider
call, persist its attempt. An interrupted request whose remote outcome is unknown
remains visibly uncertain rather than silently repeating a potentially paid call.

Decode audio on the server from a private seekable descriptor. Permit only supported
media demuxers and descriptor/pipe protocols, excluding playlists, network fetches and
external file references. Bound scratch disk, decoded duration, output bytes and each
audio chunk. Preserve original container time, including leading silence and gaps,
so imported video playback and transcript ranges share one timeline. Retain derived
blob references before writes so interruption and explicit deletion cannot orphan them.

Persist immutable version/chunk/segment provenance, raw provider usage and normalized
counters. Reuse current model admission, cancellation and budgets. Configured prices
are estimates; unknown metrics or absent prices are unknown, never an actual zero bill.
Actual invoices and representative media evaluation remain separate evidence.

Keep corrections outside generated versions and scope provider speaker identifiers to
their request. Later source ingestion reads labeled evidence through knowledge/dreaming
and reconciles corrected/deleted sources without asking for approval. Recorded speech
never enters a user turn or becomes authorization for an action.

## Durable media validation evidence

PostgreSQL-backed race tests pass for account isolation, stable initialization and
part receipts, conflicting retries, incomplete/corrupted originals, concurrent
completion, playback across parts, capture gap history and generation checks,
quota reservations through deletion, lost write receipts, resumable cleanup and
account deletion. Forward/reverse SQL both execute successfully. HTTP handler
tests confirm authorization before reading a body, cookie protection at completion,
declared body limits and private ranges. Filesystem/fake S3 tests verify original
bytes and range reads; mirror cleanup failures remain retryable. Local format,
lint and naming checks pass. The first full CI run found that the new limits made
the agent settings description exceed the tool's 24,000-character result limit.
Descriptions now keep readable JSON and offer nested subsection paths; the CLI
also describes nested inputs. Focused tests check both interfaces and secret
exclusion instead of enlarging the result limit. A final review also bounded
object-store reads even when a response body exceeds its declared range length;
a fake transport reproduces that case. The last completed CI run passed all
checks. Final review added API/web/CLI
read/update access to recording quotas, while the existing settings tool reads and
writes the same configuration fields. CLI readback preserves compatibility with
older servers. Chrome audits at 1440 by 1000 and 390 by 844 verified save/reload,
invalid-size feedback and no mobile horizontal overflow. The desktop audit caught
an overlapping hint, which is fixed. API/client regression tests, dashboard
typecheck/lint/tests/build, binary builds and full local lint pass. Recording
library UI, iOS flows, paid-provider evaluation and actual-device checks remain
outstanding, and are not supplied by this increment.

## Outcomes & Retrospective

The first provider increment is implemented. Registry admission and capability
checks, multipart references, unchanged media, timestamp normalization, token/duration
usage, malformed results, provider failures and cancellation pass fake-provider tests.
No recording UI or production media pipeline is shipped.
The first CI lint run rejected the official provider documentation hostname.
The scanner now allows that exact public hostname with an explanatory comment.
Provider capabilities have been verified from official documentation; actual billed
cost and device behavior remain unverified. All later acceptance criteria remain open.

## Parallel implementation and integration acceptance

Three isolated workstreams implement native capture/import/background controls,
web/CLI/tool surfaces, and speaker correction/reference curation with knowledge
reconciliation. The coordinator owns the worker, shared API contracts and review.
Capture originals are bounded rolling files with absolute timeline offsets and
explicit pause/interruption gaps. Versions retain each original media's provenance;
request-local speaker IDs never establish identity across files.

| Capability | Web | CLI | LLM tools | iOS |
| --- | --- | --- | --- | --- |
| Quotas, provider/transcription selection, budgets | Partial | Partial | Partial | In progress |
| Import, recoverable upload, browse/play/delete | In progress | In progress | In progress | In progress |
| Transcript versions, status/cancel/retry/reprocess | Pending integration | Pending integration | Pending integration | Pending integration |
| Confirmed labels, passage edits, references | Pending integration | Pending integration | Pending integration | Pending integration |
| Source title/date and transcript text corrections | In progress | In progress | In progress | In progress |
| Learning progress and reference invalidations | In progress | In progress | In progress | In progress |
| Capture, interruptions, locked-screen controls | Not applicable | Not applicable | Not applicable | In progress |

No row is complete until shared authorization/error checks and applicable client
checks pass. Chrome desktop/mobile interaction inspection is required for web
changes. Swift package tests cannot establish native compilation or real-iPhone
screen-lock, background-transfer, interruption and battery behavior. Merge and
deployment are tracked separately from implementation and CI.

Schema review follows existing transactional parent validation and ownership
patterns. Recordings use the singular recording namespace because their originals
belong to the account independently of an enabled agent. Account removal retains
blob references until the storage coordinator, which runs independently of agent
activation, deletes originals and derived media. Negative tests exercise foreign
owners/agents, caller-supplied parent IDs, and chunks from another version.
Account-deletion tests cover both original and derived media, failed mirror deletion
and restart of the coordinator. No new composite constraint policy is introduced.
Estimated cost uses float64 consistently with existing agent usage accounting;
unknown actual cost remains null, and provider results cannot invent actual billing.
Reverse migrations drop dependent tables before their parents.

The security check flagged WAV-size arithmetic in the local encoder. Decoder
admission already bounded its callers, but encoding now validates the byte count
and sample alignment at the allocation itself. A regression rejects oversized
and incomplete PCM without relying on caller limits; ordinary WAV headers remain
unchanged. Fresh security analysis and CI must pass before merging this increment.
## Worker and shared API checkpoint

The local worker uses existing registry admission, durable agent jobs, recording
blob ledgers and usage/budget accounting. Explicit transcription selection is off
by default. Completed media is admitted asynchronously; explicit reruns retain
stable request keys and immutable versions. Each submitted attempt retains usage,
unknown outcomes are visible and are not automatically resubmitted. Speech remains
transcript evidence and creates no conversation messages.

Shared owner-only APIs provide paged transcript reads, status/version metadata,
paid processing requests and cancellation. Cancellation retains in-flight billing
evidence while preventing new segments. Explicitly delivered audio/video attachments
can be imported or resumed against an existing matching original, with a durable
manifest committed before part writes and verified completion before acknowledgment.
Settings expose the optional model and provider-advertised file capabilities through
the existing settings path. No price or actual bill is inferred from model names.

Focused PostgreSQL/race checks pass for worker retries, unknown usage, budget
exhaustion before provider submission, cancellation, account-owned attachment imports,
unchanged playback and settings round trips. Full make lint including naming reports
zero issues. These checks do not establish end-to-end completion; raw-media decoding
through the worker, native builds, integrated surfaces, corrections/references and
real-device/provider evaluation remain integration acceptance work.

Merged durable storage is deployed from reviewed main. Service version and migration
were verified; authenticated disposable-media upload, verified receipt, playback,
idempotent retry and deletion smoke checks passed. Unfinished worker code is not
included in that deployment.
The security check flagged WAV-size arithmetic in the local encoder. Decoder
admission already bounded its callers, but encoding now validates the byte count
and sample alignment at the allocation itself. A regression rejects oversized
and incomplete PCM without relying on caller limits; ordinary WAV headers remain
unchanged. Fresh security analysis and CI must pass before merging this increment.

## Integrated backend review checkpoint

Completed runs publish to the existing knowledge source in the same transaction;
periodic bounded sweeps resume automatic voice-reference curation. Selection sends
only verified, account-owned, currently confirmed reference bytes within model
limits and rechecks identity names immediately before paid submission. Completed
chunks reuse their immutable attempts without reading newly curated references.

Same-version retry keeps completed chunks and usage. It is idempotent for an
active job and refuses stopping workers, unknown paid outcomes and results
discarded after cancellation. Explicit reprocessing remains a separate paid
version. Source title and actual occurrence date are free, generation-checked
metadata edits; a missing date stays unknown.

PostgreSQL race checks cover safe retry, stopping/uncertain/discarded requests,
foreign accounts and agents, immutable receipts and metadata changes. Focused
DB, worker, API and local decoder checks pass, including packaged-compatible
FFmpeg descriptor decoding. Full lint and naming checks pass. These local results
do not establish production worker execution, provider billing or real-device
recording behavior.

All new capabilities and settings require web, CLI, LLM tool and iOS parity.
Transcript text editing is being added as independent immutable corrections,
with unique passage alignment across reruns and existing memory reconciliation.
The original generated transcript remains available. No capability row is marked
complete while a required interface, review or device acceptance check is missing.

## Completed timeline and correction integration

The completed recording timeline joins the latest completed version of each
original in absolute timestamp order, with bounded pages and explicit run/media
provenance. A pending rerun does not hide its previous completed version. Version
history and incomplete originals remain available separately. PostgreSQL tests
verify pagination, version selection, immutable history and foreign-account refusal.

Text edits use independent immutable correction history. Both per-run and full
recording reads project effective text while retaining generated text. Source and
destination interval ambiguity is a review blocker until corrections remain
independent evidence. Confirmed speaker projection is also being centralized so
equal-time overlapping speakers cannot inherit another request-local group's name.

A bounded reference-reconciliation sweep runs before the global agent-enabled
check. Turning model work off cannot prevent stale attribution facts from being
retired. A regression verifies reconciliation continues without a registry or
provider call. Integrated recording, dreaming, memory and API PostgreSQL race
checks and full lint/naming checks pass at the current local checkpoint.

The transcript foundation (#433) and worker (#434) passed review, full lint,
Server, security and other CI checks and were squash merged. Their release
v0.142.0, commit 3cf9d4e, is confirmed running with migrations applied, no restart
loop and no pending restart. The complete private backup archive was validated
before replacement and the prior running image retained. Authenticated upload,
verified receipt, exact resume, byte-identical playback, transcript library reads,
missing-version read/cancel refusal and deletion/read refusal passed. Production
transcription is Off; refusal is not evidence of provider execution.

Raw dashboard, CLI and grouped tool surfaces (#436) passed review and all checks
and were squash merged in v0.143.0. That release is confirmed running as commit 4e41d42, with authenticated raw
upload/receipt/resume/playback/delete and existing agent/mailbox health reads passing. Native and correction/learning
increments remain in review. The native Server failure was traced to one-line
shared Swift GraphQL constants; multiline documents now expose their actual
schema dependency on the correction increment. Do not skip that check.

Review fixed overlapping speaker/text correction projection, media-local playback
seeks, account-ID upload ownership and model-Off reference reconciliation. Another
confirmed finding requires stable recording knowledge-source names: generic name
edits are refused, while human titles use recording metadata. Evidence guards and
cleanup also check source identity so stale renamed-source documents cannot survive.

A controlled 19.963-second nonprivate synthetic fixture was sent through the
existing registry to the configured API provider, once without references and once
with two five-second synthetic named references. Both returned timestamps and
reported token usage: 199 input/547 output without references, 325 input/585 output
with references (299 audio and 26 text input tokens). Neither invoice nor actual
cost is verified. Both outputs collapsed two synthetic voices into one label;
reference API acceptance is not proof of attribution quality. Representative
meeting evaluation remains required, and provider names cannot count as user
confirmation. Production configuration stayed Off throughout this evaluation.
Native package tests, simulator builds and actual physical-device evidence are
tracked separately; no real-iPhone capture or lock-screen acceptance is claimed.

The merged worker was also exercised with the real configured provider in an
isolated disposable database and authenticated HTTP API. A synthetic original
was uploaded, verified, decoded and processed into one immutable paid attempt
and five timestamped segments. The attempt reported 199 audio input and 549
output tokens; original playback remained byte-identical, and anonymous
transcript reads were refused. This is an isolated real-worker result, not
production provider activation, representative-meeting quality or invoice evidence.

The configured provider credential returned HTTP 403 from the organization
costs endpoint. Matching measured token usage to actual billing requires billing
administration access or invoice evidence; this does not block implementation.
