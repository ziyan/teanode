-- The dependent attempt retains provenance after the original sample is removed.
CREATE TABLE "recording_reference_invalidation" (
 "id" varchar(32) PRIMARY KEY,
 "recording_id" varchar(32) NOT NULL REFERENCES "recording"("id") ON DELETE CASCADE,
 "recording_transcript_run_id" varchar(32) NOT NULL REFERENCES "recording_transcript_run"("id") ON DELETE CASCADE,
 "recording_transcription_attempt_id" varchar(32) NOT NULL REFERENCES "recording_transcription_attempt"("id") ON DELETE CASCADE,
 "reference_blob_id" varchar(128) NOT NULL,
 "reference_recording_id" varchar(32) NOT NULL,
 "reference_transcript_run_id" varchar(32) NOT NULL,
 "speaker_identity_id" varchar(32) NOT NULL,
 "invalidated_confirmed_name" varchar(255) NOT NULL,
 "reference_passage_start_ms" bigint NOT NULL,
 "reference_passage_end_ms" bigint NOT NULL,
 "reference_source_generation" bigint NOT NULL,
 "invalidation_message" text NOT NULL,
 "created_at" timestamptz NOT NULL,
 "is_reconciled" boolean NOT NULL DEFAULT false,
 UNIQUE ("recording_transcription_attempt_id", "reference_blob_id")
);
CREATE INDEX "recording_reference_invalidation_pending" ON "recording_reference_invalidation" ("recording_id", "is_reconciled");
