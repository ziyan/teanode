# Recordings

A recording is a meeting kept as evidence: the original audio or video, the
transcripts made from it, and what the person said about them (who spoke,
what was really said). Why recordings are passive sources and not
conversation turns is in
`docs/decisions/20261012-recordings-are-passive-knowledge-sources.md`. This
document says how the parts a reader would otherwise guess wrong work.

## The pieces

`recording` is the row an account owns (migration `0164_recording_media.sql`),
with `recording_media` for each original and its uploaded parts. A transcript
is made per original as a `recording_transcript_run` (a version) whose
attempts and `recording_transcript_segment` passages are never changed
(migration `0165_recording_transcripts.sql`); what the person says about them is
kept beside, not in them: speaker names and the intervals they name
(`recording_speaker_identity`, `recording_speaker_correction`, migration
`0166_recording_speakers.sql`) and corrections of the words
(`recording_transcript_text_correction`, `0169`). The **timeline** is the
latest completed version of each original, with the person's corrections
projected over it (`ProjectRecordingTranscriptTextCorrections` and
`ProjectRecordingSpeakerCorrections`), and it is what every screen reads.

## Sharing

Why it is shaped like the calendar's is in
`docs/decisions/20261022-a-recording-is-shared-at-a-level.md`. This is how it
works.

**A recording is shared at a level**, with one person or with a whole circle
(`recording_share`, migration `0255_recording_share.sql`). Sharing with a person
needs the two to share a circle; sharing with a circle needs the sharer to be in
it. Both are checked when the share is made, with the circle membership rows
held, so a person leaving at the same moment takes turns with the share rather
than racing it. Only the owner shares a recording, and not one they deleted.

| level | the reader is given |
| --- | --- |
| `summary` | the title, the date, the length, the generated summary, and the names of the speakers the owner confirmed |
| `transcript` | all of that, and the transcript: the passages of the timeline as the owner reads them, with their corrections |

At no level: the audio or video and any way to play it (the media routes stay
the owner's), the voice samples and references, the contact a speaker's name is
tied to, whether a speaker is the owner, the processing history and its costs,
the words heard live that no completed version covers yet, or anything else
`models.RedactRecording` does not name.

**One place says what a reader may do:** `GetRecordingShareLevel` on the
transaction (`internal/db/database_recording_share.go`). It is the highest of
the shares that reach the reader, and the query joins `circle_member` for the
reader and for the recording's owner, so a share to a circle stops reaching a
reader when the owner leaves it, and a direct share stops when the two no longer
share one, whether or not any row has been deleted yet. It also needs the owner
and the reader to be enabled and to hold `agent:use`, the permission the
recording operations ask for, and finds nothing for a deleted recording or one
with no owner (`recording.user_id` is empty once its account is deleted).
Nothing else compares a recording's `UserID` with the caller to decide this.

**Redaction happens in one place too.** `models.RedactRecording` takes what the
owner's recording yields and a level and returns a `SharedRecording`; it copies
named fields, so a field added to a recording later is not shared until it is
named there. `ReadSharedRecording` and `ListSharedRecordings` on the transaction
are the only callers. A read gives the speaker names, found by projecting the
speakers over the whole timeline (who spoke is worked out passage by passage),
only on the first page, since a reader paging on has them already, and at
`transcript` a page of passages from an offset; a listing leaves both out,
since it would read every timeline to make a list.

When a person leaves a circle, `endRecordingSharesOutsideCircles` (called with
the other kinds from `endSharesOutsideCircles`) deletes the direct shares the
circle made possible, in both directions, and the owner's shares to the circle
they left. Deleting a recording deletes its shares.
`ListRecordingSharesBy` and `ListRecordingSharesReaching` are the two lists a
sharing overview reads.

**The surfaces** use the same operations. GraphQL: `ShareRecording`,
`UnshareRecording` and `ListRecordingShares` (the owner's), `ListSharedRecordings`
and `ReadSharedRecording` (the reader's). The command line: `teanode recording
share|shares|unshare|shared|read-shared`. The dashboard: Share in a recording's
menu, and a Shared with you panel under the library that opens a page of its own
(`/recordings/shared/<id>`). The iPhone: Share with people in a recording's menu,
and a row on the Recordings list once anybody has shared one.

**The agent's `recording` tool** has `share`, `shares`, `unshare`, `shared` and
`read-shared`. `share` is a granting call: the person is asked first unless they
allowed it, and the card says the other speakers did not agree
(`models.RecordingShareConsentWords`, the words every surface uses before a
share is made). `unshare` is a write. `shared` and `read-shared` work only in a
run the person started and not in an answer to a chat of several people
(`tools.IsStartedByPerson`, `tools.IsAnsweringInChat`); a dream or any
background run is refused before the server is asked. What they return is
another person's words: the result is untrusted and begins with
`tools.SharedRecordingMarker`, which the run that files what a conversation
taught reads as it reads a shared page, so a fact that only repeats it is
refused (`docs/subsystems/memory.md`, shared pages). Nothing from a shared
recording is kept in memory, and the knowledge sources and dreams read only the
person's own recordings.

## Things that will catch you

- A shared read and an owner's read are different operations. The owner's go
  through `GetRecording(userId, id)`, which finds only the caller's own, and a
  share changes none of them: the media, the speakers and the corrections of a
  recording are the owner's alone.
- The listing has no speaker names on purpose. A screen that wants them reads
  the recording.
- The names in a share's listing (who it is with, whose it is) are other
  people's words and are shown as names, never as instructions.
