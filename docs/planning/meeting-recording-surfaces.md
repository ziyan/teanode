# Recording surfaces execution plan

This plan supplements `meeting-recording-execplan.md`. This stream owns the
web dashboard, shared client, teanode CLI and grouped recording tool. Native
support is owned by the iOS stream. Shared storage, worker, ownership,
projection and learning APIs are coordinated with the backend streams.

## Implemented increments

The raw recording increment is merged. It provides the account-owned library,
audio/video import, bounded hashing and resumable parts, verified completion,
original playback/download and deletion. Resume verifies the retained exact
original and preserves native recording/media IDs. CLI downloads publish only
after matching the original digest and size and never overwrite an existing
file. The tool uses existing attachment delivery and never activates a remote
microphone or invents filesystem access. Evidence results remain untrusted.

The processing increment remains a separate draft. It provides explicit
transcription Off selection, known capability labels, provider/global/person
budget settings, versions and full completed timeline, paginated transcript
inspection, status/cancel/safe retry and explicit paid reprocessing. Free
metadata, speaker and recognized-text edits require the current generation.
Stable client keys are retained after uncertain responses. Generated text
remains immutable beside effective corrections and retained edit history.

Speaker names use canonical server projection: original attempt and local
speaker binding, then unique source/destination interval alignment for reruns.
Overlapping speakers, partial labels and ambiguous edits remain separate
passage evidence. Automatic provider labels remain unconfirmed. References
show their original passages; revocation warns about affected automatic
attribution without scheduling a paid rerun. User naming curates references
and reconciles memory automatically without another approval gate.

Recording sources use their human title from existing specification metadata.
Their internal name and path are not editable. Existing pause, sync, Stop
reading and schedule controls are retained. Explicit Resume reading republishes
current effective evidence after Stop reading without paid transcription.
Normal corrections and new versions preserve stopped source reading.

## Capability parity

| Capability | Dashboard | CLI | LLM tool | iOS |
| --- | --- | --- | --- | --- |
| Library/import/recovery/deletion | Implemented and audited | Tested transport and live smoke | Shared attachment/ownership actions tested | Native stream, review and device evidence pending |
| Original playback/download | Audio/video playback audited | Verified atomic download | Owned media paths and timestamps | Native stream, device evidence pending |
| Provider/model/quota/budgets | Settings audited | Shared settings and capability reads | Existing operator settings and capability reads | Native stream, review pending |
| Timeline/version/status/cancel/retry/reprocess | Implemented and audited | Live reads and shared action tests | Shared action validation and risk tests | Native stream, review pending |
| Historical citation paging/playback | Exact version/page and bounded playback audited | Live bounded passage-offset read | Same bounded operation | Native stream, review pending |
| Metadata/speaker/text corrections | Free edits and canonical warnings audited | Shared action tests and live reads | Explicit user edits only, untrusted results | Native stream, review pending |
| Reference management/invalidation | Populated references and uncertainty audited | Live evidence reads | Same evidence and revoke operation | Native stream, review pending |
| Learning/stop/resume/schedule | Actual source and resume/schedule flows audited | Live schedule set/clear and resume | Shared resume/schedule operations tested | Native stream, review pending |

No row establishes full feature completion while an applicable surface or
required real-device validation remains pending. Live LLM execution was not
performed by this stream. Existing confirmation/risk policy is retained.

## Review findings and fixes

Raw review fixed account cache isolation, URL pagination, native upload resume,
manifest validation before bytes, verified completion and atomic download.
Processing review fixed stale outer titles, interpolation conventions, scoped
speaker rename, missing unaligned text warnings and unsafe client-only speaker
inference. All known names remain available for explicit confirmation.

The Back control now follows the existing page-action button pattern.
Recording navigation uses the established Microphone icon, distinct from the
Knowledge grid. New row actions use existing icon-action/Tooltip components.
Recording source schedule editing uses the existing FormDialog and generic
source API; explicit empty Cron clears periodic scheduling and nextRunAt while
preserving pause state.

Historical citations accept canonical version and legacy run parameters, use
bounded passage-offset lookup and select the exact historical page. Missing
versions and invalid/no-match bounds show persistent feedback without falling
back to newer evidence. Fractional timestamps prevent short passages displaying
identical rounded endpoints. Passage links retain their owned media href and
play the correct existing original in-page, subtracting its timeline offset
and pausing at the exact end. Navigation stops playback and superseded play
failures cannot pause a newer selection.

## Actual validation evidence

Focused Go race checks cover shared client, CLI and recording tool. Full make
lint includes naming, configuration documentation and link-style checks. The
web type check and production/extension builds pass. Focused dashboard tests
cover raw transfer/recovery, source schedule explicit clearing, historical
citations, canonical correction projection, direct resume and media event
boundaries. Current test counts and exact head are reported with the PR.

An isolated authenticated preview accepts the nine current recording read
GraphQL documents. CLI verifies bounded historical offset, learning status,
schedule set/clear and idempotent resume with no new transcript version. These
are synthetic fixtures, not provider transcript-quality or billing tests.

Actual Chrome audits at desktop and 390-pixel phone widths inspect library,
import/resume/error dialogs, audio/video originals, metadata, speaker naming,
rename/reset, recognized-text edits/reset/history, version status/usage,
cancel/retry and explicit paid-dialog warnings. Populated synthetic references,
reference invalidation and unaligned text/speaker warnings were inspected.
Source controls expose the human title and no generic name/path editor.
Schedule save/clear preserves paused state; clearing resets nextRunAt. Stop
reading is visibly distinct from unavailable evidence, and direct Resume
reading restores current passage ingestion without another dialog or paid run.

A historical fixture with more than one transcript page opens the old version
on page two despite a newer version. Chrome observes audio start at 10.2
seconds and pause at 10.38 seconds on both layouts. Reference playback selects
its later original using the correct offset while the first original stays
paused. Imported video passage playback is checked separately. Original
receipts and source generation remain unchanged by playback.

The phone viewport/document widths match, horizontal scroll is zero and menus
are closed for content checks. Desktop and phone navigation compare the
recording microphone with adjacent existing icons. The earlier clipped phone
raw-player capture was rejected and replaced after fresh layout inspection.
Browser file selection remains blocked by the extension's file-access setting;
no extension permission was changed, and chooser completion is not claimed.

A disposable loopback provider supplies known model metadata for settings and
source UI audits and refuses execution requests. It has no external credentials.
After audits the isolated global/personal agents are restored Off, providers
removed, model assignments cleared and the preview restarted and verified.
Production provider/billing checks and deployment are owned by the root stream.

## Remaining acceptance

Rebase against the final learning API increment, rerun checks on the exact
head, review integrated changes and let CI verify the updated draft. Native
build review and applicable four-surface checks remain coordinated with iOS.
Real-iPhone capture, background transfer, interruption recovery, lock-screen
controls and battery behavior need actual device evidence. Actual provider
billing must be verified from provider evidence and must not be inferred from
provisional pricing. The separate device-setup redesign is not included here.

## Follow-up review evidence

A phone-width Chrome review reproduced the floating assistant launcher covering a transcript edit action. Recording row actions now reuse the existing pager corner clearance at narrow widths; the same edit control opened its correct dialog with the launcher visible. Desktop row layout remains unchanged. Fractional timestamps consistently retain three millisecond digits, and the text correction history explains an empty history and retained edits whose boundaries do not match a new version. Fresh desktop original-card spacing matched the normal page heading gap, so no speculative spacing change was made.

The recording navigation now uses the shared RecordIcon reviewed separately, retaining the existing icon size and wrapper. Desktop and phone navigation, standard Back button, and the text-history controls were inspected in Chrome. Safety evidence-observer integration is still pending its shared backend checkpoint; CI from the earlier head does not verify these follow-up edits.

The direct recording tool now notifies the trusted recording-evidence observer after successful owned evidence reads. Tests cover every evidence operation, safe capability/model/offset reads, and refused read responses. It remains compatible with normal human turns and does not add a learning approval gate. The shared unattended-dream guard is maintained by the backend stream. On the safety checkpoint, client/CLI/tool race tests, full naming lint, dashboard ESLint/typecheck, all dashboard and dependency tests, and production/extension builds passed. The stack still needs exact-head CI and the shared guard final checkpoint before merge.
