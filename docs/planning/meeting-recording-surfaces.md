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
| Audio/video file import | Implemented | Implemented | Attachment API integration pending | Capture stream |
| Recoverable original upload | Exact manifest resume | Exact manifest resume | Attachment API integration pending | Capture stream |
| Delete recording | Confirmation dialog | Confirmation/force | Destructive risk | Capture stream |
| Transcription model/providers/budgets | Existing settings, explicit model pending | Existing settings, explicit model pending | Existing settings, explicit model pending | Native settings pending |
| Transcript versions/status/cancel/reprocess | Worker API integration pending | Pending | Pending | Capture stream |
| Metadata and occurrence date editing | API integration pending | Pending | Pending | Capture stream |
| Speaker corrections/reference management | Learning API integration pending | Pending | Pending | Capture stream |

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
results with no microphone operation. Go race tests and dashboard unit tests
pass. Full local lint, including naming checks, has been run successfully;
repeat it after the final changes before committing.

Chrome desktop and phone inspection has exercised empty/import forms, retained
synthetic media and actual audio playback. The browser extension refuses file
selection because its file access permission is disabled. Do not claim an
end-to-end browser chooser/upload test or enable broader permissions silently.
Final resume/error layouts are being repeated after rebuilding. Audio and video
originals import through the CLI and verified downloads match the source.
Browser audio playback advances; a video codec unsupported by the browser now
shows download guidance rather than silently appearing as audio. Subsequent
transcript/correction/settings interactions remain pending.

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
