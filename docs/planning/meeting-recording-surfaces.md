# Recording surfaces execution plan

The account-owned recording API is shared by the dashboard, command line,
agent tool and native app. This stream implements the first three surfaces;
native support is coordinated with the capture stream. This plan supplements
`meeting-recording-execplan.md` and does not change its completion criteria.

## Current increment

- Dashboard library, unchanged audio/video import, bounded hashing and part
  upload, verified completion, native original playback/download and deletion.
- Resume reselects and verifies the exact retained original. Existing native
  capture originals resume their existing recording/media IDs, rather than
  becoming a second imported recording.
- CLI `recording list/read/limits/import/download/delete` uses the shared client.
  Import always retains the local source. Download publishes only after the
  whole original digest and size match and refuses to overwrite a destination.
- The recording tool lists, reads and deletes owned recordings and returns
  private playback paths. Results are marked untrusted. Delete uses existing
  destructive confirmation handling. Microphone activation is never offered.

## Capability parity

| Capability | Dashboard | CLI | LLM tool | iOS |
| --- | --- | --- | --- | --- |
| Library and original metadata | Implemented | Implemented | Implemented | Capture stream |
| Original playback/download | Implemented | Verified download | Private playback path | Capture stream |
| Audio/video file import | Implemented | Implemented | Delivered attachment import, live smoke pending | Capture stream |
| Recoverable original upload | Exact manifest resume | Exact manifest resume | Exact attachment manifest resume, live smoke pending | Capture stream |
| Delete recording | Confirmation dialog | Confirmation/force | Destructive risk | Capture stream |
| Transcription model/providers/budgets | Explicit Off and known capability display | Settings and capability read | Existing operator settings and capability read | Native stream, review pending |
| Timeline/versions/status/cancel/retry/reprocess | Free edits and status inspected; paid processing pending | Authenticated reads verified | Shared actions tested, live agent execution pending | Native stream, review pending |
| Metadata and occurrence date editing | Free edits and status inspected; paid processing pending | Authenticated reads verified | Shared actions tested, live agent execution pending | Native stream, review pending |
| Speaker corrections/reference management | Canonical projection and free edits audited; populated references pending | Authenticated read verified | Shared actions tested, live agent execution pending | Native stream, review pending |
| Recognized-text edit/reset/history | Free edits and status inspected; paid processing pending | Authenticated reads verified | Shared actions tested, live agent execution pending | Native stream, review pending |
| Learning/invalidated-reference evidence | Free edits and status inspected; paid processing pending | Authenticated reads verified | Shared actions tested, live agent execution pending | Native stream, review pending |

No row constitutes end-to-end completion while an applicable surface is pending.

## Review and validation evidence

Code review found and fixed stale account data display, missing URL pagination,
and exact-source resume accidentally creating a new import for native media.
The recording page remounts on account changes, aborts outstanding uploads and
checks current account/source IDs before displaying cached query results.

Meaningful tests cover lost upload responses after storage succeeds, reuse of
verified parts, refusal of a different original/account on resume, corrupted or
oversized downloads, whole-file receipt validation, independent incremental
SHA-256 vectors around padding and transport boundaries, and untrusted tool
results with no microphone operation. Review also added malformed-server
manifest tests that require zero original-part/completion requests and atomic
download tests preserving existing destinations even on corrupt responses.
Go race tests and 14 dashboard tests pass. Full local lint, including naming checks, has been run successfully;
repeat it after the final changes before committing.

Chrome desktop and phone inspection has exercised empty/import forms, retained
synthetic media and actual audio playback. The browser extension refuses file
selection because its file access permission is disabled. Do not claim an
end-to-end browser chooser/upload test or enable broader permissions silently.
Final resume, deletion-dialog and deleted-source error layouts were inspected
at desktop and phone widths after rebuilding. Audio and video originals import
through the CLI and verified downloads match the source. Browser audio and
compatible video playback advance; an unsupported video track now shows
download guidance rather than silently appearing as audio. An early phone
audio screenshot is clipped and is rejected evidence; fresh Chrome inspection confirmed a 390-pixel viewport and document, zero horizontal scroll, and a closed menu. A replacement phone screenshot was independently inspected. The integrated interface checkpoint below records subsequent interactions.

## Next increments

Integrate the shared attachment import and transcript worker APIs. Add the
explicit transcription model with known capability display, preserve unknown
pricing rather than presenting a guessed cost, and use existing budget controls.
Then add version/status/cancel/explicit paid reprocessing and metadata editing
across all surfaces. Integrate confirmed speaker correction/reference APIs and
the recording knowledge source display. Review every increment and inspect each
web interaction at desktop and phone widths before it is considered ready.

Real-iPhone capture, background transfer, interruptions, lock-screen controls
and battery checks belong to the native stream and remain unverified without
actual device evidence. Actual provider billing is also unverified.

## Processing interface work in progress

A separate follow-up implements explicit transcription model selection (blank is
Off) and known audio capabilities. CLI settings read the new model without
breaking reads against older servers; the existing operator settings tool uses
the redacted configuration and common settings mutation.

Shared action validation now powers CLI and tool operations for versions,
bounded transcript pages, initial processing, safe same-version retry,
cancellation, explicit paid reprocessing, delivered attachment import,
metadata, speaker confirmation/rename/reset, reference revocation, recognized
text edit/reset/history, learning counts and reference invalidation evidence.
Free edits require the current source generation. Stable paid/correction keys
are required and retained after an uncertain response. Metadata date is
explicit so a title edit cannot silently replace the occurrence date.

The dashboard implements these processing and editing controls with original
passage playback and immutable generated text alongside effective corrections.
Canonical server projection confirms speakers with original attempt and local
speaker binding. Reprocessed segments require unique source and destination
intervals; ambiguous or partial corrections remain separate evidence. Clients
never label equal-time overlapping speakers by interval alone. Automatically returned speaker labels remain unconfirmed.
Recording knowledge sources have a human label and retain pause/sync/removal
controls while excluding the unrelated generic path editor.

These interfaces are not yet accepted as ready: live contract smoke, desktop
and phone Chrome interaction checks, integrated review, complete naming lint,
iOS parity checks and the new correction backend integration remain required.
Focused Go race tests and 20 dashboard tests currently pass; no paid provider
call or real-phone validation has been performed.

The full recording timeline interface uses the latest completed version per
original and chronological absolute timestamps. Incomplete originals and newer
pending versions are explicitly excluded, with per-run inspection retained for
status and billing evidence. The aggregate API is an integration dependency.
Chrome inspected transcription Off selection and successful save at desktop
and phone widths. Phone measurements show matching viewport/document width,
zero horizontal scroll and the main content at the left edge. The integrated checkpoint below records the processing and correction controls
that have since been exercised; fixture-only and device gaps remain explicit.

Integrated review found and fixed passage playback seeking: absolute segment
and reference bounds subtract the selected original's timeline offset before
seeking. A regression covers a later capture chunk. Review also removed
client time-only speaker inference in favor of canonical server projection;
equal-time overlapping speakers and ambiguous reruns cannot inherit a name
from another group. The UI test verifies confirmation and unresolved interval
warnings. Formatting tests caught incorrect interpolation braces and those
were corrected to the existing catalog convention.


## Integrated interface audit checkpoint

The integrated preview now accepts the actual recording documents for versions,
per-run and aggregate transcripts, speakers, text history, learning, reference
invalidations and processing availability. The timeline selects the completed
original version and excludes newer failed, canceled and uncertain versions.
Nine authenticated CLI read operations also passed against the same backend.
These checks use synthetic transcript fixtures, not provider transcription.

Actual Chrome desktop and phone interactions verified metadata editing, speaker
group naming and rename, recognized-text editing with generated originals,
text reset with retained history, selected speaker-passage reset, version usage
inspection, queued cancellation, and the safe retry admission error while
processing is off. Equal-time overlapping speakers retained separate attribution.
Free API text edits also passed stable-key retry and immutable-original checks.
The phone viewport and document both measured 390 pixels, with zero horizontal
scroll and the main content starting at zero. Corrections and actions wrap
without clipping. Review fixed the outer recording title staying stale after a
metadata save, and restricted rename to identities linked to this recording.
All account identities remain available as name suggestions for confirmation.
Unaligned text and speaker corrections have explicit projection warnings.

The source metadata exposes the human recording title. Global Agent Off keeps
the raw library reachable but blocks the existing source settings page. Enabling
only that flag is rejected by existing validation because a default model and
enabled provider are required; no provider was enabled to bypass this rule.
The source-control visual check, populated voice references and invalidations,
paid processing confirmation with a configured model, and nonempty capability
labels still need isolated fixtures. Browser file selection remains blocked by
extension file access. No paid call, actual billing, live LLM execution or
real-iPhone validation is claimed by this stream.
