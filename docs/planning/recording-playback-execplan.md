# Browser-compatible recording playback

This ExecPlan covers the playback gap found while reviewing native capture for #423.

## Purpose and constraints

Native capture retains recoverable CAF originals, while Chrome cannot decode that
container through its audio element. Preserve those originals and their receipt
digests. Prepare a separate owned WAV preview through the existing descriptor-only
decoder and blob store, with deletion and quota handling shared with other derived
recording bytes. Preparation does not call a model or require transcription to be On.

## Contract and implementation

`PrepareRecordingPlayback(recordingMediaId)` explicitly prepares local playback.
`ReadRecordingPlayback(recordingMediaId)` returns the owned verified preview or null.
The response identifies the media, playback path, content type, byte size, digest,
duration in milliseconds and original media timeline start. GET/HEAD at the owned
media playback path supports byte ranges and the existing account permission checks.

Normalize audio using the existing decoder, including silence for delayed video
audio. Keep bounded chunks in memory, unlinked scratch files, bounded duration and
output size, cancellation and limited concurrent preparation. Register the derived
blob before storage writes so failed writes and source/account deletion remain
recoverable. Verify original bytes before decoding and derived bytes before publishing.

The dashboard uses the original when supported and prepares compatible audio only
after a playback request. CLI and LLM tools share client documents and ownership/error
semantics. iOS retains native original playback and adds the compatible path where
applicable. A preview never authorizes releasing a retained local original.

## Progress

- [x] Inspect native CAF capture, original range serving and decoder/blob conventions.
- [x] Agree on the shared playback API and source-clock semantics.
- [x] Implement bounded decoding, owned reservation and durable preview publication.
- [x] Add authenticated range serving and meaningful ownership/cleanup/integrity tests.
- [ ] Integrate web, CLI, tools and applicable iOS support.
- [ ] Verify CAF and WAV behavior in actual Chrome desktop and mobile layouts.
- [ ] Review findings, run full lint/naming and required CI, then merge and deploy.

## Validation and limits

Use invented nonprivate fixtures. Check delayed video alignment, unchanged original
digests, byte-range reads, denied owners, incomplete uploads, quotas, cancellation,
concurrent preparation and deletion races. Run packaged-reader checks as well as local
tests. Actual signed iPhone recording, recovery and lock-screen behavior remain device
checks; browser preview validation cannot replace them.

## Implementation checkpoint

The backend prepares local descriptor-only WAV audio with a process-wide concurrency
bound, a ten-minute cancellation deadline and a two-gibibyte original/output bound.
The existing recording blob table accepts the new purpose without a migration.
Owned reservations precede persistent bytes; failed writes and missing derived storage
can resume under an already reserved quota. Account and source deletion use the existing
cleanup worker. Raw media and upload receipts are unchanged.

Actual PostgreSQL race tests passed for ownership, related parents, concurrent quota,
identical retries and deletion. Packaged descriptor-reader tests with PostgreSQL passed
for CAF conversion, unchanged originals, WAV digest and header, absolute timeline,
ranges, reduced-quota storage repair and original corruption refusal. Existing delayed
video and cancellation checks passed. Authenticated HTTP/query tests passed with the
agent disabled, including HEAD, byte ranges, If-Range, denied owners and deletion.
Review found and fixed a reduced-quota retry refusal before reservation reuse.

Actual Chrome reproduced unsupported CAF and playable WAV; this establishes the
codec gap, not yet the integrated dashboard fallback. Web, CLI, LLM tools and applicable
iOS integration remain underway. No merge or deployment of this increment is claimed.
