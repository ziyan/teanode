# Native recording execution plan

The recording library is account-owned, independent of an agent conversation.
Capture is explicit and silent. No recording activates a tool or grants permission.

## Current implementation

- Native raw-media API, exact part and whole-file receipt checks.
- Account-scoped recovery journal, synthetic failure and ownership tests.
- Operator original/account quotas and explicit file-transcription model, alongside
  existing provider configuration and token/cost budget controls.
- Local ten-minute linear PCM CAF originals, split at pause and interruption.
  Each original has its own stable media ID and absolute meeting timeline offset.
  Gaps occupy meeting time without expanding silent media.
- Files and share-extension imports preserve bytes; a shared private container keeps
  originals available after the extension exits.
- File-backed background upload tasks, receipt-based resume, and local retention on
  missing, incorrect or failed receipts. Uploads begin after capture stops.
- Native paged library, retained local and verified remote original playback, metadata
  editing, per-original transcript versions and a full chronological recording timeline.
- Processing, cancellation, safe same-version retry and explicit paid reprocessing.
  Paid run keys remain stable through an unknown response.
- Confirmed group or selected interval labels, automatic reference state and original
  reference-passage playback/revocation. Submitted correction drafts survive relaunch.
- Free recognized-text edits and immutable original/version history, effective text
  projection and reset. Unknown response drafts retain their exact key and payload.
- Learning/source progress, existing source pause/sync/Stop reading controls, reference
  invalidation warnings and explicit raw deletion remain separate native operations.
- Deferred capture transitions retain recorder identity across journal saves. Pending
  stop or interruption wins over rollover; stale callbacks cannot start another segment.
- Upload owners are queued across retained profiles. Signout cancels their file tasks
  and prevents a suspended metadata operation from scheduling another upload.
- Live Activity elapsed time and explicit pause/resume/stop intents bind to the local
  session ID so an old activity cannot control a new session.
- Recording-specific system intents require a matching active Live Activity before
  lock-screen resume. Foreground resume can create one when permitted. This follows
  [Apple recording intent documentation](https://developer.apple.com/documentation/AppIntents/AudioRecordingIntent); it still needs real-device evidence.

## Checks and review

Swift package tests run in the Linux Swift container. They cover account isolation,
retaining unverified originals, pause/interruption history, rejected completion
receipts, and corrected interval attribution across reruns including explicit removal.
No test contacts a real server or provider.

The initial native increment passed the Xcode simulator build, including the widget
and share targets. The follow-up text/timeline/recovery code still needs a fresh native
build. Simulator screenshot collection has not completed, so no layout inspection is
claimed. The package suite passed 305 tests after the text, timeline and coordination edits.
Full project lint passed, including naming, and the secret scan reported no secrets.

Native compilation and simulator screenshots require macOS CI. Screens need light,
dark and largest Dynamic Type inspection, with VoiceOver ordering checked separately.
Physical iPhone lock-screen audio, accessory/call interruptions, crash recovery,
background relaunch, network loss, battery and storage impact remain device checks.
No physical-device result or actual provider bill has been measured here.

## Remaining acceptance work

- Review and fix native compilation, simulator layout and interaction findings.
- Integrate shared APIs and compare authorization, generation conflicts, idempotency,
  metadata, cancel/reprocessing and deletion errors against web, CLI and tools.
- Check long import, interrupted transfer and relaunch against the authenticated server.
- Verify pause boundaries, media clock alignment, retained manual labels, reference
  invalidation and learning reconciliation end to end.
- Record real-iPhone evidence and representative meeting/billing evaluation before the
  parent feature is considered complete.


## Native parity matrix

| Capability | Native implementation | Remaining evidence |
| --- | --- | --- |
| Provider, transcription, request capabilities and storage quotas | Existing operator forms extended; explicit off assignment | Fresh SDK build and form layout inspection |
| Existing global/person token and cost caps | Existing budget forms retained | Integrated authorization checks |
| Browse, Files/share imports, local and remote playback | Native library and players; byte-preserving local retention | Simulator layout; real-device transfer and video alignment |
| Versioned and full timeline transcripts, processing and usage | Paged native views; estimate and verified actual cost distinct | Integrated provider run and real billing |
| Cancel, safe retry and explicit paid reprocess | Native actions; stable paid key | Shared error/auth tests and request recovery |
| Speaker group/interval correction and reference management | Native confirmation, history, original passage player and revoke | End-to-end curation and device playback |
| Recognized text edit/history/reset | Independent immutable correction APIs and retained request drafts | Fresh native build and shared projection integration |
| Learning and reference invalidations | Existing source controls and provenance warnings | Integrated reconciliation evidence |
| Metadata and raw deletion | Native generation-aware title/time, explicit deletion | Account isolation smoke against integrated server |
| Local capture and lock-screen controls | Rolling originals, durable recovery and recording-specific intents | Physical iPhone lock/interruption/relaunch evidence |
