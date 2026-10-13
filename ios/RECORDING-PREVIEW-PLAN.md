# Native compatible recording playback

## Purpose and contracts

Preserve supported original audio/video playback, including recoverable CAF capture.
An explicitly opened original that the native player cannot decode may use the owned
compatible WAV preview. Preparation uses the local decoder and existing blob lifecycle,
without a provider call. Raw downloads and durable upload receipts stay unchanged.

PrepareRecordingPlayback and ReadRecordingPlayback return the original media identity,
owned playback path, content type, size, SHA-256, duration and absolute timeline start.
The native client checks that metadata against the selected immutable original before
an authenticated download. A size/hash mismatch never becomes a codec fallback.
Passage playback preserves the existing trimmed composition and original time offset.

## Execution and validation

- Add typed read/prepare transport and receipt checks, matching existing native APIs.
- Load actual asset playability asynchronously. Keep supported video visual; use a
  clearly labeled compatible audio fallback for unsupported media only.
- Verify downloaded derivative size/hash before playback; keep all local originals.
- Preserve account/session cancellation and private temporary-file cleanup.
- Test foreign paths/media, changed clocks and malformed receipts before any download.
  Verify pending/read-only cache responses and refusal semantics with fake transport.
- Run portable tests, real built-schema document checks and full lint including naming.
  Use exact macOS SDK builds and inspect actual light/dark/largest simulator images.
- Review findings before publishing a draft implementation PR. Backend integration and
  production deployment are coordinated separately. No physical-device behavior is
  claimed from portable tests or unsigned simulator captures.

## Status and dependencies

The previous native recording increment is merged and passed all required CI, with
39 actual simulator captures inspected. Unsigned shared-container behavior cannot
prove capture/share, and real-iPhone interruption/background/lock-screen tests remain
pending. This new increment starts from published main and uses the agreed preview
contract. Real schema checks require the matching shared backend implementation;
missing APIs must remain a visible dependency rather than a skipped schema check.

The typed transport and native fallback are implemented. Original receipt validation
finishes before probing codec support; unsupported user Play always prepares
compatible audio, verified independently before using the same bounded player.
Current account identity, retained profile and cancellation are rechecked across
awaits. A silent local WAV demo covers the actual preview UI without network access.
327 portable tests, the real built-schema document check and full lint pass. The
simulator SDK build passed at the preceding checkpoint. Fresh final captures and
SDK checks include both original-ready and compatible-preview player controls.

Review found that reading cached metadata alone could never reach the server repair
operation after corrupt preview bytes. Unsupported user Play now always prepares
locally, which verifies/repairs the cache using the existing reservation. Supported
video avoids preparation. A production transport-selection regression covers both
paths. Asset failures fall back only for an unsupported format/decoder, not unrelated
permission or I/O errors. Ready verified original and preview files offer explicit
whole-file Share actions; no automatic external sharing or journal release occurs.

Ready supported originals also expose an explicit Use audio preview action, so CAF
and other supported native formats can export compatible WAV without pretending
they are unsupported. Default supported video playback stays visual. The action
retains the original player on failure and uses the same verified download, bounded
composition, visible-session cancellation and whole-file Share controls.

Review also found that supported video could not return from audio preview without
reopening playback. Compatible mode now offers Use original only when that original
was proven playable. It re-downloads and verifies the original, preserving the
preview on failure and restoring original sharing on success. A separate rendered
route verifies both return and full-preview sharing controls at all three appearances.
