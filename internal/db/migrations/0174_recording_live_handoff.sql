-- Where a piece of a live capture starts reading, when the speech at the end of
-- the piece before it ran into the cut: the first piece's audio is preceded by
-- the tail of the previous piece's, from this timeline millisecond on.
ALTER TABLE "recording_transcript_run" ADD COLUMN "handoff_start_ms" bigint CHECK ("handoff_start_ms" >= 0);

-- A speaker of a recording still being captured, known only inside that
-- recording. A speaker label from the provider means something inside one
-- transcription request only, so each piece of a live capture would otherwise
-- call the same person by a different letter. The sample is a clip of this
-- speaker's voice sent with later pieces under the label, so the provider
-- answers with the same label again. It is never sent for another recording.
CREATE TABLE "recording_temporary_speaker" (
 "id" varchar(32) PRIMARY KEY,
 "recording_id" varchar(32) NOT NULL REFERENCES "recording"("id") ON DELETE CASCADE,
 "speaker_label" varchar(64) NOT NULL,
 "recording_blob_id" varchar(128) REFERENCES "recording_blob"("id") ON DELETE SET NULL,
 "source_attempt_id" varchar(32) NOT NULL REFERENCES "recording_transcription_attempt"("id") ON DELETE CASCADE,
 "source_local_speaker_id" varchar(255) NOT NULL,
 "passage_start_ms" bigint NOT NULL CHECK ("passage_start_ms" >= 0),
 "passage_end_ms" bigint NOT NULL CHECK ("passage_end_ms" - "passage_start_ms" BETWEEN 2000 AND 10000),
 "speaker_identity_id" varchar(32) REFERENCES "recording_speaker_identity"("id") ON DELETE SET NULL,
 "created_at" timestamptz NOT NULL,
 UNIQUE ("recording_id", "speaker_label"),
 UNIQUE ("source_attempt_id", "source_local_speaker_id")
);
