# Spell a person's own names and terms right in recording transcripts

This ExecPlan is a living document. Keep Progress, Surprises & Discoveries, Decision Log and Outcomes & Retrospective current as work proceeds. It builds on `docs/planning/meeting-recording-execplan.md` (the passive recording feature) and `docs/planning/recording-ux-redesign-execplan.md` (generated titles and summaries), both checked in; read the Decision Logs there first. This is server, CLI and LLM tool work only: nothing under `web/` changes except the one run-kind label described below, and nothing under `ios/`. The dashboard and phone get their interface in a later change.

## Purpose / Big Picture

Speech recognition mishears the words that matter most to one person: "Tea node" for TeaNode, "off line" for offline, a colleague's name spelled wrong. The provider knows nothing of the person's world. After this change the server builds a glossary from the person's own data (never from the transcript), gives the provider a short vocabulary hint where the provider accepts one, and after a transcript finishes runs a correction pass that fixes misheard glossary terms. Each fix is stored as a text correction with the origin `glossary`, so the recognized text stays in history, the fix shows in passages, summaries and dreams, and the person can undo it. A correction the person typed always wins, and a fix the person undid is not made again.

To see it working: put a page named "Ferncroft" in the agent's memory, transcribe a recording whose speech was recognized as "Fern croft", and read `teanode recording text-corrections --recording-id ...`: a correction with `textCorrectionOrigin` `glossary`, `glossaryTerm` `Ferncroft`, `originalPhrase` `Fern croft` and `correctedPhrase` `Ferncroft` appears, the transcript reads "Ferncroft", and the recording is then named from the corrected text.

## Progress

- [x] (2026-10-13) Read the repository instructions, both recording plans, the summary job, the text correction code and the memory graph, contact and calendar readers.
- [x] (2026-10-13) Migration `0172_recording_glossary_corrections` with reverse file, model fields, database operation `ApplyRecordingGlossaryCorrections`, person-wins and rejection rules, tests.
- [x] (2026-10-13) Glossary builder `internal/agent/recording_glossary.go` and vocabulary hint, with tests.
- [x] (2026-10-13) Provider hint: `llm.TranscriptionRequest.VocabularyPrompt` sent as `prompt` except for models whose name contains "diarize"; request body tests for both cases.
- [x] (2026-10-13) Job `recording_terms` (`internal/agent/recording_terms.go`) through the run machinery, validation, completion hook ordering, tests with a fake model.
- [x] (2026-10-13) CLI and LLM tool parity (origin and glossary term shown), run-kind label keys in the dashboard catalogues.
- [x] (2026-10-13) Build, vet, gofmt, lint and tests for the touched packages; branch pushed.

## Surprises & Discoveries

- The base branch has `0171_recording_import_duration`, so `0172` follows it directly. `0172` was already applied to a database before review, so the folded-fix flag is the separate migration `0173_recording_glossary_correction_folded` rather than an edit of `0172`.
- `CreateAddressBook` and `CreateCalendar` ignore `AgentGranted`; it is stored only by the update calls. The glossary only reads books and calendars the person granted their agent, so tests grant them with `UpdateAddressBook` and `UpdateCalendar`.
- Two glossary fixes to one passage made in one batch got the same source generation, and the projection (newest generation first) could pick the superseded one. Each fix now takes the next generation, as a person's correction does.
- The default recording model `gpt-4o-transcribe-diarize` is the one that must not get a prompt, so the hint reaches the provider only when an operator configures a plain transcription model. The correction pass is therefore the main mechanism, the hint a bonus.
- A request body in the fake model's log has the fence tags JSON-escaped; tests decode the body and look at the user message.

## Decision Log

- Decision: the glossary is read from the owner's data only: pages of the agent's memory graph of kinds self, person, organization, project, topic, place and thing (not folders or periods; not dormant), most recently modified first, with their aliases; the display names of contacts in address books the person granted their agent; summary and named attendees of events in calendars the person granted their agent that overlap the recording's `recordedAt` plus or minus one hour; and the recording title when its source is `user`. Capped at 400 terms, each 2 to 80 characters on one line, no duplicates compared without regard to case. Rationale: only what the person keeps is trusted; the grants are the existing rule that nothing from an ungranted source goes to a model. Date: 2026-10-13.
- Decision: the page path is not a term, only the page name and aliases. Rationale: paths are lowercase slugs ("people/maren-plover") that spell nothing the person would say. Date: 2026-10-13.
- Decision: order of priority is typed title, calendar, memory pages by recency, contacts. The vocabulary hint is the first terms in that order, at most 40 terms and 700 characters (about 200 tokens). Date: 2026-10-13.
- Decision: the pass is the job kind `recording_terms` (15 characters; `agent_job.kind` is `varchar(16)`), subject the recording, run on the work kind `compact` (the fast model, else the default), and every model call is `oneShot` with the job's own run, so each call is a recorded run conversation with usage and budget accounting, visible in the runs list. No direct provider call and no hand-written usage record. `RequireBudget` is still checked first, as the summary job does. Date: 2026-10-13.
- Decision: ordering. The transcript completion hook calls `queueRecordingTerms`, which queues the terms job, and queues the summary job directly only when the terms job cannot be queued (agent off, no model, no way to act as the person). The terms handler always queues the summary job on its way out, including when it skips (no glossary, no passages), fails, or finds the recording gone, from a context that outlives the job's deadline. The handler never returns the pass's error, so a retry cannot delay naming. Date: 2026-10-13.
- Decision: the transcript goes in pieces of about 24,000 characters (`splitRecordingPieces`, never truncated) inside the existing untrusted-data fence, and the glossary goes in a fence of its own, introduced as a list of spellings and not instructions, because calendar, contact and memory names can come from outside. Every proposal is validated: the segment is one this pass read, the original phrase occurs in the segment's current text as whole words (white space in the text matches the single spaces of the phrase), the glossary term is in the glossary (the glossary's own spelling is stored), the corrected phrase, once the words it shares with the original at either end are set aside, is the term and nothing else (case, spaces and punctuation aside), the words it replaces read like the term (letters edit distance 0 for a term of 3 letters or fewer, at most 1 for 5 or fewer, a 0.6 similarity above that), and its length is at most twice the original plus 20 characters (both at most 200 characters, no control characters). Hostile speech that persuades the model to propose anything else produces proposals that fail these checks. Date: 2026-10-13.
- Decision: a glossary fix replaces every whole-word occurrence of the original phrase in the passage's current effective text and is stored as a whole-passage text correction (`original_transcript_text` is the recognized text, `corrected_transcript_text` the new text), the same shape as a person's correction, so projection, history, knowledge reconciliation and dreams need no new path. `ApplyRecordingGlossaryCorrections` checks everything again under the recording lock and applies a batch with one knowledge sync. Date: 2026-10-13.
- Decision: person wins. A glossary fix is never created on a passage with an active person correction (same segment, or the same media and interval in another transcript version). When a person corrects a passage, active glossary fixes on it are marked removed. When two glossary fixes hit one passage, the earlier is marked removed and the later starts from the text the earlier produced. Marking leaves a row's source generation alone so later rows sort newer. Date: 2026-10-13.
- Decision: rejection is the removed row itself. A glossary fix is not made when a glossary fix with the same `glossary_term` (without regard to case) was removed on the same segment or an overlapping interval of the same media, whatever words it replaced, so a rerun that reads "T node is" cannot bring back what was undone as "T node". A fix removed because a later fix took it in has `is_folded` set (migration 0173) and does not count. When the person removes a glossary fix, the fixes folded into it become undone again. Removing through the existing text-reset path therefore needs no new operation.
- Decision: the run conversations of the `recording_terms` and `recording_brief` jobs store the transcript in their prompt, so they are deleted with the recording (`deleteRecordingRunConversations`, called when it is deleted and again at cleanup, matching conversations by kind `run`, subject the recording and the job kind). Date: 2026-10-13.
- Decision: the provider hint goes as the `prompt` form field for every model whose name does not contain "diarize". Date: 2026-10-13.
- Decision: no new GraphQL operation. The text-correction model gains `textCorrectionOrigin`, `glossaryTerm`, `originalPhrase`, `correctedPhrase`; the shared client document for `ReadRecordingTranscriptTextCorrections` asks for them, which gives the CLI (`recording text-corrections`) and the tool (`text-corrections`) the same output. Reading a passage's effective text is unchanged. Date: 2026-10-13.

## Outcomes & Retrospective

The server side is complete and tested with a fake model. Not verified: a real provider, a real transcript on a deployed server, and the dashboard and phone interface for undoing a glossary fix (the operation exists as `text-reset` and `RemoveRecordingTranscriptTextCorrection`; the interface is a later change).

## Context and Orientation

The server is Go. A recording is `models.Recording` (`internal/models/recording.go`). A transcript run is `models.RecordingTranscriptRun`; its passages are `RecordingTranscriptSegment` rows, each with an id, a time interval and the recognized `TranscriptText`. A text correction (`internal/models/recording_text_correction.go`, table `recording_transcript_text_correction`) never rewrites that text: it is a row saying that for this segment the text reads differently. `ProjectRecordingTranscriptTextCorrections` in `internal/db/database_recording_text_correction.go` fills each segment's `EffectiveTranscriptText` from the newest correction for it (or, for a later transcript version, from one with the exact same interval). Corrections change the source generation of the recording and call `SyncRecordingKnowledge`, which reconciles the passages that dreaming learns from.

The agent's job queue and worker live in `internal/agent`. A job kind is registered in `internal/agent/agent.go` (`self.Register`) and named in `internal/models/agent.go`. `oneShot` in `internal/agent/thinking.go` runs one model turn without tools in a run conversation of its own, recording usage under the job kind. `fenced` in `internal/agent/ask.go` wraps untrusted text in the `<untrusted-data>` fence. `internal/agent/recording_summary.go` is the job that names a recording after its transcript; this change queues it after the new pass and does not edit it. The memory graph is `models.AgentNode` rows read with `ListAgentIndex`; contacts are `ListAddressBooks` and `ListContacts`; calendar events are `ListCalendars`, `ListOccurrences`, `GetCalendarObject` and `calendar.Parse` for attendees.

The CLI is `internal/cmd/recording_actions.go`, the tool is `internal/agent/tools/recording/recording.go`, and both reach the API through the documents in `internal/client/recording_processing.go`. Migrations are `internal/db/migrations/NNNN_name.sql` with a `.reverse.sql`, described in `docs/coding/database-migrations.md`. A test that needs PostgreSQL takes it from `internal/db/dbtest`, which uses `TEANODE_TEST_DATABASE_HOST`.

## Plan of Work

Milestone 1, storage. Add migration 0172: on `recording_transcript_text_correction`, `text_correction_origin` varchar(16) not null default 'person' checked to `person` or `glossary`, `glossary_term`, `original_phrase` and `corrected_phrase` text not null default ''. The reverse deletes glossary rows (the older schema cannot describe them) and drops the columns. Add the fields to the model, set the origin on person corrections, add `ApplyRecordingGlossaryCorrections`, the supersede rule in `CorrectRecordingTranscriptText`, and the rejection check.

Milestone 2, glossary and provider hint. `internal/agent/recording_glossary.go` builds the terms and the hint. `llm.TranscriptionRequest.VocabularyPrompt` is written by `internal/llm/openai_transcription.go` as `prompt` unless the model name contains "diarize"; `transcribeRecordingChunk` fills it from the glossary and logs and continues without it on a failure.

Milestone 3, the job. `internal/agent/recording_terms.go`: queue function, completion hook, handler, prompt, validation. Register the kind. Add the run-kind label to `web/src/agentRuns.ts` and the three catalogues, which `TestEveryAgentJobKindHasALabel` requires.

Milestone 4, parity and tests. The shared client document asks for the new fields; the CLI and tool descriptions explain origin, that glossary fixes came from the person's own notes and that the person may undo them with text-reset.

## Concrete Steps

From the repository root, start PostgreSQL as the Makefile test target does and run the tests with the vendor directory:

    docker run -d --rm -e POSTGRES_DB=teanode -e POSTGRES_USER=teanode -e POSTGRES_PASSWORD=teanode -e POSTGRES_HOST_AUTH_METHOD=trust pgvector/pgvector:pg17
    export TEANODE_TEST_DATABASE_HOST=<container ip>
    go build ./...
    go vet ./internal/agent ./internal/db ./internal/llm ./internal/models ./internal/client ./internal/cmd
    gofmt -l internal
    go test -mod=vendor ./internal/agent/... ./internal/db/... ./internal/llm/... ./internal/models/... ./internal/client/... ./internal/cmd/... ./internal/api/...

Stop the container afterwards. `make test` leaves gofmt rewrites under `vendor/`; this plan used direct `go test`, so none were made.

## Validation and Acceptance

Run the tests above; every package passes. The tests that prove the behavior: `TestGlossaryCorrectionIsProjectedAndKeepsTheOriginal`, `TestPersonCorrectionBlocksAndSupersedesGlossaryCorrection`, `TestRemovedGlossaryCorrectionIsNotAppliedAgain` and `TestGlossaryCorrectionMigrationAddsAndDropsTheColumns` (internal/db); `TestBuildRecordingGlossaryUsesOnlyThePersonsOwnData`, `TestValidateRecordingTermFixRejectsWhatIsNotASmallGlossaryFix`, `TestRecordingTermsJobAppliesGlossaryFixesAndQueuesTheSummary`, `TestRecordingTermsJobIgnoresWhatHostileSpeechTellsTheModelToDo`, `TestRecordingTermsJobLeavesAPersonCorrectionAlone`, the skip and failure tests, and `TestTranscriptCompletionQueuesTheTermsJobBeforeTheSummary` (internal/agent); `TestTranscriptionSendsTheVocabularyPromptOnlyToModelsThatAcceptIt` (internal/llm).

## Idempotence and Recovery

The migration only adds columns with defaults. Its reverse removes glossary rows, which are regenerable by running the pass again. The pass can run any number of times: a fix whose phrase is no longer in the text, or that was undone, is skipped. A failure leaves the transcript as it was and naming still happens.

## Artifacts and Notes

The model is asked for this shape and nothing else:

    {"termCorrections":[{"segmentId":"...","originalPhrase":"Fern croft","correctedPhrase":"Ferncroft","glossaryTerm":"Ferncroft"}]}

The CLI and tool show each correction with `textCorrectionOrigin` (`person` or `glossary`), `glossaryTerm`, `originalPhrase` and `correctedPhrase`.

## Interfaces and Dependencies

`models.TextCorrectionOrigin` with `TextCorrectionOriginPerson` and `TextCorrectionOriginGlossary`; four fields on `models.RecordingTranscriptTextCorrection`. `db.RecordingGlossaryCorrectionInput` and `Transaction.ApplyRecordingGlossaryCorrections(userId, recordingId string, proposals []RecordingGlossaryCorrectionInput) ([]*models.RecordingTranscriptTextCorrection, error)`. `models.AgentJobRecordingTerms` = "recording_terms". `llm.TranscriptionRequest.VocabularyPrompt string`. In `internal/agent`: `buildRecordingGlossary`, `recordingVocabularyPrompt`, `validateRecordingTermFix`, `(*Agent).QueueRecordingTerms`, `queueRecordingTerms`, `runRecordingTerms`. No new dependency.

Revision note (2026-10-13): first version, written with the implementation, recording the decisions taken while building it.
