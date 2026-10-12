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
- [ ] CI and independent review of the durable media increment.
- [ ] Increment 2b: transcript runs/versions and reconciliation jobs.
- [ ] Increment 3b: background transcription with existing budgets and usage accounting.
- [ ] Increment 4: recording transcript knowledge source, automatic dreaming
  and corrections/deletion that reconcile dependent memories.
- [ ] Increment 5: speaker/group/passage correction and automatic voice clips.
- [ ] Increment 6: dashboard import, playback and transcript correction.
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

Run dashboard typecheck/lint/behavior tests and inspect at phone width. Run Swift
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

The active increment is feat/recording-storage, based on current main. Work covers
account-owned recording/media manifests, bounded resumable part uploads, verified
whole-file receipts, streaming playback and durable explicit-deletion cleanup.
The service work is merged. The original working copy is preserved.
Subsequent increments will add processing/usage, sources/reconciliation, corrections
and references, dashboard and native capture. Provider billing and actual iPhone
behavior remain open evidence requirements. The issue must remain open until those
acceptance criteria are observed.

## Durable media validation evidence

PostgreSQL-backed race tests pass for account isolation, stable initialization and
part receipts, conflicting retries, incomplete/corrupted originals, concurrent
completion, playback across parts, capture gap history and generation checks,
quota reservations through deletion, lost write receipts, resumable cleanup and
account deletion. Forward/reverse SQL both execute successfully. HTTP handler
tests confirm authorization before reading a body, cookie protection at completion,
declared body limits and private ranges. Filesystem/fake S3 tests verify original
bytes and range reads; mirror cleanup failures remain retryable. Local format,
lint and naming checks pass. Full CI, dashboard/iOS flows, paid-provider evaluation
and actual-device checks are not evidence supplied by this increment.

## Outcomes & Retrospective

The first provider increment is implemented. Registry admission and capability
checks, multipart references, unchanged media, timestamp normalization, token/duration
usage, malformed results, provider failures and cancellation pass fake-provider tests.
No recording UI or production media pipeline is shipped.
The first CI lint run rejected the official provider documentation hostname.
The scanner now allows that exact public hostname with an explanatory comment.
Provider capabilities have been verified from official documentation; actual billed
cost and device behavior remain unverified. All later acceptance criteria remain open.
