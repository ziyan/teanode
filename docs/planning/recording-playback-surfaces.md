# Compatible recording playback surfaces

This is the client execution companion for the passive meeting recording plan.
The [shared recording plan](meeting-recording-execplan.md) and
[backend preview lifecycle](recording-playback-execplan.md) remain in their own
stream. Originals stay retained and unchanged; unsupported browser containers
can have a locally prepared audio preview without a provider request. Supported
originals also offer an explicit audio-only choice without changing their default player.

## Behavior and boundaries

Keep a supported original video or audio player by default. Offer an explicit
Use audio preview action for supported originals; a browser playback error offers
Play compatible audio instead. Use original returns to a supported player without
preparing again and retains ready preview metadata and download. Do not offer
return to an original already known to be unsupported. Preparing writes owned
blob storage and counts against existing storage bounds; it is not a read-only
operation, and it incurs no transcription/provider charge. Never prepare a
preview during metadata loading. Ready previews use the owned media playback
route and retain that original's absolute recording timeline offset. Label an
unsupported-video fallback as audio only and preserve original downloads.

Expose preparation, ready metadata and verified preview download in the CLI,
shared client and grouped recording tool. CLI read-only refuses preparation.
The tool classifies preparation as a write and ready metadata as an evidence
read, notifying the existing trusted observer. Native parity belongs to the
iOS stream; do not claim physical-device verification from browser playback.

## Execution checkpoints

- [x] Read current player, passage alignment, raw download and tool risk patterns.
- [x] Reproduce synthetic CAF versus WAV decoding in actual Chrome.
- [x] Add shared typed preview contracts, validation and bounded verified download.
- [x] Add CLI and LLM tool parity with existing owner/errors/read-only semantics.
- [x] Add explicit compatible audio controls with safe lifecycle and passage alignment.
- [x] Review integrated behavior, run meaningful tests and full naming lint.
- [x] Inspect actual Chrome desktop and phone preparation/playback/errors.
- [x] Open a separate draft increment with diagrams.
- [ ] Verify exact-head CI after the merged backend rebase and final mode controls.

## Validation

Test preview identity/path receipt mismatches before download, size/digest
verification and atomic destination preservation. Test no preparation from
mount/preload/error alone; only explicit user intent may write. Check stale
prepare responses cannot change a different original/account, passage offsets
and end pause stay correct after switching to audio preview, supported video
keeps its original visual player, and failures offer an explicit retry without
paid transcription. Inspect actual desktop and phone layouts with synthetic
owned originals. Report backend tests, merge/deployment and native device
checks separately from surface compilation and browser evidence.

## Review and integration evidence

Independent review found no confirmed issue in the surface risk, receipt,
bounded download, atomic file publication or explicit playback paths. Preparation
is a write, ready metadata is an untrusted owned-evidence read, and the trusted
observer is notified before the tool returns that read. Failed downloads never
replace an existing destination. Supported original video retains its player;
compatible fallback audio is labeled separately and has its own ready download.

Actual Chrome reproduced unsupported CAF and supported WAV decoding from the
same synthetic audio. A packaged local descriptor reader then served owned
synthetic recordings through the normal API with the assistant, providers and
transcription model off. Explicit desktop Play prepared and played compatible
audio. On the phone, selecting a passage at 7.2 to 7.8 seconds from an original
starting at 7 seconds prepared compatible audio and paused at 0.8 seconds in
that media. Desktop and phone widths measured 1440 and 390 without overflow.
Unreadable source data exposed the local error and retained explicit retry and
original download. Supported historical WAV and video stayed on their original
routes and stopped at their selected end times. Settled screenshots were
inspected; captures taken during viewport transitions were rejected.

Final actual Chrome desktop and phone checks also switched supported video to
optional audio and back. The visual original returned paused, ready preview
download remained available, and known unsupported CAF had no unusable return
action. Review fixes separated playback mode from ready metadata and disabled
return while preview metadata was loading.

The CLI refused preparation in read-only mode before transport. Ready preview
metadata and download worked in read-only mode and matched the receipt digest
and byte size. An actual Chrome preview download matched the same receipt.
Original bytes, digest and source generation remained unchanged. Existing
synthetic correction/reference/citation fixtures were retained. No model or paid
provider call was made by these checks, and no live server was changed.

The final increment passes full naming lint, dashboard lint/types, 469 dashboard
tests plus 2 dependency regressions, 12 focused player/controller checks, Go races
and production/extension builds. Mode regressions cover original/preview
switching, ready-download retention and unsupported return controls. Both complete preview selections validated against the integrated
schema and executed through actual owned requests. The whole dashboard query
script still reports unrelated interpolation/fragment and disabled-service
errors on this off fixture, so it is not claimed to pass. Exact-head CI and
native preview integration remain required before completion. Native implements
explicit audio preview, original return and verified sharing; its portable tests
and simulator build pass. Physical sharing and playback remain unverified. Browser playback
is not physical iPhone capture, upload, installation or codec validation.

## Surface parity

| Capability | Dashboard | CLI | LLM tool | iOS |
| --- | --- | --- | --- | --- |
| Explicit local preparation | Optional audio choice or unsupported original/passage Play | prepare-playback or explicit download --preview | prepare-playback, write risk | Optional audio choice or unsupported original Play |
| Ready owned preview | Labeled audio player, supported original return | read-playback | read-playback, evidence read | Labeled audio player, supported original return |
| Ready preview download | Separate ready audio download retained on return | Bounded digest-verified download --preview | Returns owned ready path, existing handoff | Verified original/audio sharing |
| Original and timeline preservation | Original downloads, supported video, offset/end pause | Original default unchanged | Existing owned IDs and absolute timestamps | Original downloads, video, offset/end pause |

Root owns backend merge/deployment and native coordination. This surface
increment is reviewable independently but does not establish four-surface or
physical-device completion on its own.
