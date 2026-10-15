# One label for one voice across the pieces of a recording

This ExecPlan is a living document. Keep Progress, Surprises & Discoveries, Decision Log and Outcomes & Retrospective current as work proceeds. It builds on `docs/planning/meeting-recording-execplan.md` and `docs/planning/recording-live-transcription-execplan.md`; read their Decision Logs first.

## Purpose / Big Picture

A recording captured on a phone is transcribed in pieces of about thirty seconds. Every voice heard gets a temporary label ("Speaker 3") and a short sample of that voice, and each later piece is sent up to four samples so the provider answers with the same label. In a meeting of more than four people a voice whose sample was not sent comes back under a new letter, and the curation gives it a new label: one person ends up as "Speaker 3" in one piece and "Speaker 9" in another, and has to be named twice.

After this change, when a new label is made, the server asks the provider whether its voice is one of the recording's labels that were not sent with that piece. If the provider answers with an existing label, the new label is merged into it: its passages read as the existing label (or its name), naming either names both, and its sample is not sent again. A merge can be undone from any of its passages ("Separate from Speaker 3"). The person chose fully automatic merging (2026-10-14); separation is the undo.

To see it working: capture a meeting with five or more voices. In the transcript, one voice keeps one label across pieces; a passage whose label came from a merge offers "Separate from Speaker N" in its menu on the dashboard and the phone, and `teanode recording speaker-separate --recording-id ... --speaker-label "Speaker 9"` does the same.

## Context and Orientation

- `internal/agent/recording_temporary_speakers.go`: `curateRecordingTemporarySpeakers` runs after every piece and creates a `models.RecordingTemporarySpeaker` (table `recording_temporary_speaker`) for each unknown local speaker, with a clean sample clip when one exists (`createRecordingTemporarySpeaker`).
- `internal/agent/recording_references.go`: `recordingSpeakerReferences` picks the samples sent with a piece (at most four, one per name).
- `internal/db/database_recording_speaker_projection.go`: `ProjectRecordingSpeakerCorrections` gives each passage its label, recognized identity and corrections. A passage belongs to a temporary speaker when it is the speaker's source passage, or the provider answered with the label in a piece the sample was sent to.
- `internal/db/database_recording_temporary_speaker.go`: `NameRecordingTemporarySpeaker` writes a speaker correction for every piece the speaker was heard in (keys prefixed `temporary-speaker-<speaker id>-`) and links the identity. `UndoRecordingSpeakerNaming` undoes it.
- `internal/agent/recording_speakers.go`: voice samples are cut only from passages confirmed as one speaker (`isConfirmedAsOneSpeaker`, PR #493): never from pieces where only the provider said who spoke.
- `internal/agent/recording.go` transcribes a piece with `selection.Transcriber.Transcribe(ctx, &llm.TranscriptionRequest{...SpeakerReferences...})` and records usage with `RecordUsageIn(..., string(models.AgentJobRecording), usage)`.
- Surfaces: GraphQL resolvers in `internal/api/v1api/apigraph/recording_speaker.go`; CLI `internal/cmd/recording_actions.go` with documents in `internal/client/recording_processing.go`; agent tool `internal/agent/tools/recording/recording.go` (operations list); dashboard passage menu in `web/src/recording/` (find where "speaker-undo"/`UndoRecordingSpeakerNaming` and the temporary naming are called); iPhone `ios/TeaNode/Sources/RecordingConversationView.swift` / `RecordingTranscriptView.swift` and `ios/TeaNodeKit/Sources/TeaNodeKit/Recordings.swift`.

## Plan of Work

Milestone 1, storage. Migration `NNNN_recording_temporary_speaker_merge` (next free number; read `docs/coding/database-migrations.md`) adds `merged_into_temporary_speaker_id varchar(32) NULL REFERENCES recording_temporary_speaker(id) ON DELETE SET NULL` and `merge_checked_at timestamptz NULL` to `recording_temporary_speaker`, with a reverse file. The model gains `MergedIntoTemporarySpeakerID string` (json `mergedIntoTemporarySpeakerId`) and `MergeCheckedAt *time.Time`. Database operations:
- `MergeRecordingTemporarySpeaker(userId, recordingId, speakerLabel, intoSpeakerLabel string) error`: under the same locks as naming, refuses (ErrInvalidArguments) when either is missing, they are the same, the target resolves to the source, or the source is named; points the source at the root of the target's chain; advances the source generation. It writes no correction: a label merged into a named speaker reads as that name through the projection, as recognized rather than confirmed, since the provider made the merge and not the person (changed in review, 2026-10-14).
- `SeparateRecordingTemporarySpeaker(userId, recordingId, speakerLabel string) error`: clears the pointer, marks removed the corrections the merge wrote for this speaker's appearances, advances the source generation. Separating a speaker that is not merged is ErrInvalidArguments.
- `MarkRecordingTemporarySpeakerMergeChecked(userId, speakerId)`.
- `NameRecordingTemporarySpeaker` and `UndoRecordingSpeakerNaming` take in the appearances of every speaker merged into the named one.

Milestone 2, projection and samples. In `ProjectRecordingSpeakerCorrections`, a passage matched to a merged temporary speaker reads as the root of its chain (label, or identity once named) and gets a new field `MergedTemporarySpeakerLabel` (json `mergedTemporarySpeakerLabel`) holding its own label, empty otherwise. `recordingSpeakerReferences` skips merged speakers. `isConfirmedAsOneSpeaker` keeps ignoring merged appearances (a merge is the provider's judgement, not the person's).

Milestone 3, the check. In `curateRecordingTemporarySpeakers`, after the new labels of a piece are made, for each unnamed, unmerged temporary speaker with a sample and no `merge_checked_at`: candidates are the recording's other root speakers with a sample that were not sent with the new speaker's source piece (`attempt.reference_blob_ids`) and that do not speak in that piece, most recently heard first, at most four. With no candidate, mark it checked and stop. Otherwise send the new speaker's sample clip (already cut, 2.4 to 6 seconds) as the media of one `Transcribe` request with the candidates' samples as `SpeakerReferences` under their labels. If every passage of the answer carries one candidate's label, merge into it; anything else (a letter, two labels, an error from the provider) means no merge. Mark it checked either way, so each label costs one check. Record usage under `models.AgentJobRecording` with `RecordUsageIn`, check the budget first the way a piece is (`RequireBudget` or whatever the recording job uses), and skip the check when transcription is unavailable. After a merge, queue the knowledge sync and the summary refresh the way naming does.

Milestone 4, operations. GraphQL `SeparateRecordingTemporarySpeaker(recordingId, speakerLabel)` returning the same view naming returns, queueing knowledge sync and summary like naming; CLI `teanode recording speaker-separate --recording-id ID --speaker-label LABEL`; agent tool operation `speaker-separate` (same arguments as `temporary-speaker-name`); `mergedTemporarySpeakerLabel` added to the passage fields the CLI and the tool print. Document the CLI command in `docs/reference/command-line.md`.

Milestone 5, interface. Dashboard and iPhone: in a passage's menu, when `mergedTemporarySpeakerLabel` is set, an item "Separate from <label shown>" that calls the operation and shows a toast on success or failure (dashboard: existing toast; iPhone: the existing error handling of the menu's other items). Strings in en, zh, ja on both. Screenshots: add a demo passage with a merged label to the iPhone demo recording and to the dashboard's preview data if the neighbors do, and look at the menu in light, dark and the largest text.

## Validation

- Database tests: merge, chain resolution, refusal cases, naming a merged root names both, undoing naming, separating removes the merge's corrections and only those.
- Agent tests with the fake provider of `recording_live_fixture_test.go`: five voices over pieces so one voice gets a second label; the check merges it when the fake answers with the earlier label and does not when it answers with a letter; a checked speaker is not checked again; usage is recorded; the merged sample is not sent.
- `make lint-ci`, `go test` of touched packages, web `tsc` and tests, iOS CI with screenshots.

## Progress

- [x] (2026-10-14) Milestone 1, storage: migration 0188, the model fields, `MergeRecordingTemporarySpeaker`, `SeparateRecordingTemporarySpeaker`, `MarkRecordingTemporarySpeakerMergeChecked`, naming and undo taking in merged labels. Tests in `internal/db/database_recording_temporary_speaker_merge_test.go`.
- [x] (2026-10-14) Milestone 2, projection and samples: `MergedTemporarySpeakerLabel`, merged labels read as their root, merged samples are not sent.
- [x] (2026-10-14) Milestone 3, the check: `internal/agent/recording_speaker_merge.go`, run at the end of every curation. Tests in `internal/agent/recording_speaker_merge_test.go`.
- [x] (2026-10-14) Milestone 4 (operations, CLI, tool, docs): GraphQL `SeparateRecordingTemporarySpeaker`, CLI and tool `speaker-separate`, `mergedTemporarySpeakerLabel` in the timeline document, `docs/reference/command-line.md`.
- [x] (2026-10-14) Milestone 5 (dashboard, iPhone, screenshots): "Separate from <speaker>" in a bubble's menu on both. A SwiftUI context menu cannot be opened from a launch argument, so the iPhone screenshots show the merged passage in the `recording-capture-live` demo screen, not the open menu.
- [ ] Review, deploy, watch the next real capture.

## Surprises & Discoveries

- Two curations can run at once (the one a finished piece starts in the background and one a naming starts), and both found the same new label unchecked and asked the provider twice. `MarkRecordingTemporarySpeakerMergeChecked` therefore claims the check (the first caller wins, a second gets `ErrAlreadyExists`) and the agent claims before it asks, not after the answer.
- Separating marks the merge's corrections removed rather than deleting them, so merging the same label into the same named speaker again finds those rows under the same key and returns them without writing new ones. The passage still reads as the name (through the root's identity), only without the "confirmed" mark. Naming, undoing and naming again with the same name behaves the same way already.
- `NameRecordingTemporarySpeaker` in the GraphQL layer returns a boolean, not a view, so `SeparateRecordingTemporarySpeaker` returns a boolean too.

## Decision Log

- Decision: merging is automatic, undone by separation. Rationale: the person's choice, 2026-10-14. Date: 2026-10-14.
- Decision: the check sends the new label's existing sample (no new audio cut) and at most four candidates, once per label. Rationale: one request of about six seconds of audio costs well under a cent, and the label's sample is already a clean single-voice clip. Date: 2026-10-14.
- Decision: a merge needs every passage of the answer under one candidate label. Rationale: a short clip of one voice should come back as one speaker; anything mixed is not evidence enough, and a wrong merge is worse than a duplicate label. Date: 2026-10-14.
- Decision: a named speaker is never merged into another; a new speaker may merge into a named one and then reads as that name. Samples are never cut from merged appearances. Rationale: the person's naming wins, and only the person's confirmation may teach a voice. Date: 2026-10-14.

- Decision: the corrections a merge writes for a named root are keyed by the root's speaker id (`temporary-speaker-<root id>-<attempt id>-<name digest>`), so `UndoRecordingSpeakerNaming` needed no change: its prefix match finds the root's own and the merged labels' corrections together. Separating finds the merge's corrections by that key plus the merged label's piece and local speaker label. Date: 2026-10-14.
- Decision: a merge always points at the root, and merging a label that others were merged into moves them to the new root, so a chain is never longer than one step. Naming or merging by a merged label's name resolves to its root. Date: 2026-10-14.
- Decision: a label that is already merged cannot be merged again (`ErrInvalidArguments`); the person separates it first. Date: 2026-10-14.
- Decision: the candidates of a check are only labels created before the one being checked, so two labels created by the same piece never merge into each other and a merge always goes from the newer label to the older. Date: 2026-10-14.
- Decision: a candidate is sent under the name the provider is sent for it in a piece, which is the confirmed name once the speaker is named and the label before, and the answer is read back through the same names. Date: 2026-10-14.
- Decision: a blank passage in the answer is ignored, as a piece's is; an answer with no passage left, an error from the provider, or a request that cannot be built after the claim all count as "no merge" and the label is not asked about again. A check that cannot be made now (no registry, transcription unavailable, budget spent, the model cannot take speaker references, the label's sample not yet written) is not claimed and is tried at the next curation. Date: 2026-10-14.
- Decision: the usage of a check is recorded under the model of the transcript run, once per answered request, with `RecordUsageIn`; the authority and budget check is `recordingAuthority`, the one a piece uses. Date: 2026-10-14.
- Decision: the internal pointer and the check time on `models.RecordingTemporarySpeaker` have `json:"-"`, like the other internal fields, instead of the `mergedIntoTemporarySpeakerId` the plan named; clients read `mergedTemporarySpeakerLabel` on the passage. Date: 2026-10-14.
- Decision: the tool and CLI operation is `speaker-separate` (not `temporary-speaker-separate`) as the plan said, and takes `recording_id` and `speaker_label`. Date: 2026-10-14.

## Outcomes & Retrospective

(to be written)
