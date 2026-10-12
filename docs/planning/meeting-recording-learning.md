# Recording speaker corrections and learning

This implementation uses account-owned speaker identities and recording-owned
correction intervals. A whole request-local group confirmation is expanded into
its exact media time intervals. Generated transcript versions remain immutable.
A later correction wins by source generation. Removing the newest confirmation
keeps its removal recorded, so an older name is not silently restored.

Voice references are automatic local derivations after confirmation. Selection
rejects known overlapping speakers, short passages, nearly silent audio, clipping
and high measured background noise. The conservative signal screening is not a
guarantee that diarization is correct. Unsuitable passages produce no sample.
Several clips may be retained per confirmed identity, with an account quota and
source/version/time provenance. Revoked sample metadata records its interval even
after bytes are removed, preventing the same automatic selection from returning.
A committed derived-blob reservation precedes storage writes, so retries and source
or account deletion can find unfinished writes. No provider call or approval gate
is part of naming, local curation or knowledge publication.

Recordings publish the latest completed version of each media file into the
existing knowledge source, document, chunk and dream digest stores. The trusted
learning prompt distinguishes proposals, decisions, questions and commitments;
quotes remain untrusted evidence and cannot authorize tools. Unknown occurrence
dates stay unknown. Imported upload time is not substituted for meeting time.
Partial speaker corrections keep their exact ranges without inventing word timing.

Changed evidence retires dependent facts through the existing strike/revision
mechanism and invalidates page overview inputs for the normal rewrite. It preserves
page prose/history according to existing overview handling. Raw recording deletion removes
its knowledge source and documents after reconciliation. Existing source Stop reading
keeps learned pages; retained evidence identifiers allow later raw deletion or correction
to reconcile those facts even after indexed documents are removed. Learning status exposes existing source timestamps/errors and pending/digested passage
counts, without claiming useful memories or human review. Publication pages immutable
segments in bounded batches; speaker evidence reads exclude large provider receipts.
A dream checks retained
recording/document evidence under source locks before page preparation and atomic
fact filing, preventing an old model response from recreating corrected attribution.

## Verification

- PostgreSQL race tests cover account isolation, generation conflicts, idempotent
  confirmation retries, correction preservation across rerun admission, related
  media/version validation, reference quota, durable revocation and migration reversal.
- Correction and deletion retire stale attribution while retaining revision history.
- Stale evidence after a selected-passage correction or deletion is refused.
- Signal tests reject silent, short, continuously noisy and clipped samples.
- A hostile transcript cannot escape its prompt evidence block; original quote text
  remains available for evidence verification.
- Full local lint, including the installed naming checker, passed with no findings.

## Integration acceptance still required

Root integration owns provider reference inputs and the bounded curation recovery
tick. The web, CLI, LLM tool and iOS workstreams must all expose confirmed labels,
selected passage corrections, identity rename, correction removal and reference
revocation with matching ownership, generation and error semantics. Web interactions
require actual Chrome inspection at desktop and phone widths. Native build/simulator
checks and real iPhone recording/background behavior are separate checks. None of
these interfaces, production deployment, real meeting quality or actual provider
billing is established by this backend increment alone.

## Conditioning reference corrections

An accepted transcription attempt retains its sample blob IDs. Reference revocation,
identity rename, speaker correction removal and raw-source deletion now add separate
invalidation records to dependent attempts before sample provenance disappears.
Generated speech remains unchanged. Existing knowledge documents mark automatic
attribution uncertain while retaining independent user confirmations. Reconciliation
retires dependent facts through the same revision machinery, including paused sources.
A bounded recovery query exposes pending recordings independently of ingestion being
turned on. Missing agents complete reconciliation without blocking later candidates.

Review fixed a lock order issue: paid admission, reference mutations and clip byte
verification acquire the account owner before source and reference rows. PostgreSQL
race regressions cover cross-recording dependencies for all four invalidations and
same-source admission versus deletion. Migration reversal and reapplication pass.
Text editing, four-surface interactions, deployment and real-device evidence remain
separate milestones.

## Recognized text editing

Text correction rows reference the original generated segment, run, media, exact
interval and original wording. Stable client IDs permit safe retries. Source generation
checks reject stale edits; removal preserves history and blocks older wording fallback.
The shared projection exposes generated text and effective text separately. Reruns
inherit an edit only for a unique exact media/time interval. Changed boundaries or
overlapping equal-time speakers retain an independent correction document with original
provenance and uncertain alignment. No generated segment is rewritten, and edited text
remains passive evidence under the existing trusted learning prompt.

PostgreSQL race tests pass for account isolation, retry conflicts, stale generations,
immutable originals, correction removal, unique rerun alignment, ambiguous overlap,
independent evidence and fact retirement. Real blob curation tests exercise six clean
clips for five confirmed identities: provider inputs use at most four distinct names,
respect smaller model capabilities, exclude other accounts, and reject references
revoked after preparation. Full local lint and naming have no findings. Dashboard,
CLI, tool and native editing interactions still require their separate parity checks.

## Integrated overlap review

Review reproduced two bugs: original equal-time overlapping speakers could receive
one time-only user label, and a merged rerun could inherit one original speaker's text
edit. Shared speaker projection now binds the original attempt and local speaker.
Rerun projection requires unique exact intervals in both original and destination
versions; competing active corrections remain independent evidence. Partial or
unaligned speaker confirmations retain original request/version/time citations and
cannot become full-passage labels on another speaker. Generated speech is unchanged.
Negative PostgreSQL race tests reproduce original overlap, a merged destination,
competing text correction origins, and independent original confirmation evidence.

A name change retains usable verified voice bytes under the updated confirmed identity;
it does not revive explicitly revoked clips. Review also fixed retry verification that
used a clip's historical generation after a source metadata edit. Real blob-store tests
prove renamed samples remain selectable, curation recovery succeeds, and explicit sample
refusals survive another rename. Renaming to the same name is now a metadata no-op.

Recording knowledge sources retain their stable internal names. Existing specification
settings now carry recordingId and recordingTitle for display across clients. Free title
edits update this metadata without re-enabling paused reading or changing source IDs.
Semantic settings comparison avoids extra source revisions from JSONB whitespace.
PostgreSQL tests verify quoted titles, paused state and unchanged-sync stability.

## Internal source identity review

The generic linked-source editor previously allowed recording source names to change,
although recording publication uses a stable internal name. The shared API now refuses
that rename and points users to recording title metadata. Evidence validation rejects
renamed source snapshots. Raw deletion also locates automatic sources by their existing
recording specification, so renamed or duplicate source caches cannot retain transcript
bytes. Direct database regression tests reproduce the old rename path and verify both
stale-memory rejection and cleanup. Pause and schedule operations retain existing source
semantics; display titles stay in specification settings.

## Recovery and schedule review

Completed transcripts from before automatic source support are discovered in bounded,
account-aware pages. SQL-only initialization rechecks the recording lock, active account,
agent permission and existing source history. Paused sources and retained document mappings
from Stop reading prevent automatic recreation. Global and personal model configuration
are not enabled by this recovery. PostgreSQL race tests verify separate owners, repeat
sweeps without duplicate documents, cursor progress, paused and deleted sources, account
disablement, permission removal and ownership rejection.

Recording ingestion now uses the existing source schedule calculation. An empty schedule
stays unscheduled; configured cron expressions determine the next run. Review also found
revoked clips were only cleaned by the agent worker, which does not exist while globally
disabled. Bounded revoked-reference discovery and owner-first locked cleanup now support
always-running storage maintenance. Failed deletion retains the blob reservation for
retry; successful deletion retains the revoked interval and provenance. The curation
caller uses the same lock order. Focused PostgreSQL race tests pass, and full local lint,
including naming, reports no findings. Shared maintenance wiring and CI remain integration
checks before this increment is ready to merge.

A disposable browser fixture used a nil provider registry: a short synthetic interval
produced no reference. Two explicitly named nonoverlapping synthetic intervals passed the
waveform filters and produced two clips. Normal revocation and cleanup left one usable
clip, one durable refusal and one conditioned-attribution invalidation. A merged synthetic
version retained unaligned text and speaker corrections as independent evidence. This is
UI and lifecycle evidence, not real-voice quality evidence or provider billing evidence.
Browser inspection is recorded by the surface workstream; no real-iPhone claim is made.

## Stopped reading, citations and attribution safety

Corrections and completed reruns preserve Stop reading using the retained document mapping;
obsolete dependent facts are retired without silently recreating the source. Empty or
incomplete transcripts do not create an automatic source. The derived hasStoppedReading
field distinguishes this choice from evidence that is not ready. ResumeRecordingLearning
explicitly recreates the source from current corrected evidence, subject to ownership and
source generation. It requests no paid transcription, preserves existing paused controls,
and works as SQL-only publication with models disabled. Repeat, stale, foreign-owner,
paused-source and current-correction regressions pass.

Knowledge citations now use the existing version query parameter and exact media intervals.
An owned indexed passage-offset lookup selects the original version's page without loading
transcript bodies. Tests cover more than two pages, equal-time overlap, invalid bounds,
no matching passage, ownership, missing/deleted originals and the built GraphQL schema.
Browser fixtures include separate historical and newer synthetic versions beyond one page.
The surface workstream records actual Chrome inspection of these links.

A split correction can give distinct user names to subpassages of one wrongly merged
request-local label. Generated text and labels stay immutable; the whole segment remains
uncertain, both selected intervals survive, and old whole-speaker memory attribution is
retired through existing fact revisions. Curation rejects overlapping segments, duplicated
local labels, cross-request overlap and uncertain/incomplete source versions. These gates
are rechecked when reference metadata is committed. A narrow local helper performs the
same screening without constructing a model client or starting an agent worker; a durable
completion marker makes newly completed evidence eligible for recovery.

Real blob-store tests with global models off verify split boundaries, separate synthetic
samples, overlap refusal, uncertain-source refusal and completion recovery. Accepted
samples establish retained provenance and waveform screening, not verified model identity.
The reported provider collapse of two voices, both with and without reference conditioning,
remains unresolved quality evidence. Real-human multi-speaker and real-iPhone validation
remain separate acceptance requirements; no claim of completed quality validation is made.
