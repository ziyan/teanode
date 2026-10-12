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
- Native library, original playback, metadata editing, transcript versions and
  usage/status/cancel/reprocessing controls, confirmed speaker and reference management.
- Live Activity elapsed time and explicit pause/resume/stop intents bind to the local
  session ID so an old activity cannot control a new session.

## Checks and review

Swift package tests run in the Linux Swift container. They cover account isolation,
retaining unverified originals, pause/interruption history, rejected completion
receipts, and corrected interval attribution across reruns including explicit removal.
No test contacts a real server or provider.

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
