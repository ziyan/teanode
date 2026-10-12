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
