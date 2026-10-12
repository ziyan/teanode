CREATE TABLE "recording_transcript_text_correction" (
 "id" varchar(32) PRIMARY KEY,
 "recording_id" varchar(32) NOT NULL REFERENCES "recording"("id") ON DELETE CASCADE,
 "recording_media_id" varchar(32) NOT NULL REFERENCES "recording_media"("id") ON DELETE CASCADE,
 "recording_transcript_run_id" varchar(32) NOT NULL REFERENCES "recording_transcript_run"("id") ON DELETE CASCADE,
 "recording_transcript_segment_id" varchar(32) NOT NULL REFERENCES "recording_transcript_segment"("id") ON DELETE CASCADE,
 "client_correction_id" varchar(128) NOT NULL,
 "passage_start_ms" bigint NOT NULL CHECK ("passage_start_ms" >= 0),
 "passage_end_ms" bigint NOT NULL CHECK ("passage_end_ms" > "passage_start_ms"),
 "original_transcript_text" text NOT NULL,
 "corrected_transcript_text" text NOT NULL,
 "source_generation" bigint NOT NULL,
 "created_at" timestamptz NOT NULL,
 "is_removed" boolean NOT NULL DEFAULT false,
 UNIQUE ("recording_id", "client_correction_id")
);
CREATE INDEX "recording_transcript_text_correction_passage" ON "recording_transcript_text_correction" ("recording_media_id", "passage_start_ms", "passage_end_ms");
