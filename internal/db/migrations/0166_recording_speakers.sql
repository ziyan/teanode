ALTER TABLE "recording" ADD COLUMN "speaker_curated_generation" bigint NOT NULL DEFAULT 0;
CREATE TABLE "recording_speaker_identity" (
 "id" varchar(32) PRIMARY KEY,
 "user_id" varchar(32) NOT NULL REFERENCES "user"("id") ON DELETE CASCADE,
 "confirmed_name" varchar(255) NOT NULL,
 "created_at" timestamptz NOT NULL,
 UNIQUE ("user_id", "confirmed_name")
);
CREATE TABLE "recording_speaker_correction" (
 "id" varchar(32) PRIMARY KEY,
 "recording_id" varchar(32) NOT NULL REFERENCES "recording"("id") ON DELETE CASCADE,
 "recording_media_id" varchar(32) NOT NULL REFERENCES "recording_media"("id") ON DELETE CASCADE,
 "recording_transcription_attempt_id" varchar(32) NOT NULL REFERENCES "recording_transcription_attempt"("id") ON DELETE CASCADE,
 "speaker_identity_id" varchar(32) NOT NULL REFERENCES "recording_speaker_identity"("id") ON DELETE CASCADE,
 "client_correction_id" varchar(128) NOT NULL,
 "local_speaker_id" varchar(255) NOT NULL,
 "passage_start_ms" bigint NOT NULL CHECK ("passage_start_ms" >= 0),
 "passage_end_ms" bigint NOT NULL CHECK ("passage_end_ms" > "passage_start_ms"),
 "source_generation" bigint NOT NULL,
 "created_at" timestamptz NOT NULL,
 "is_removed" boolean NOT NULL DEFAULT false,
 UNIQUE ("recording_id", "client_correction_id", "passage_start_ms", "passage_end_ms")
);
CREATE TABLE "recording_voice_reference" (
 "id" varchar(32) PRIMARY KEY,
 "recording_id" varchar(32) NOT NULL REFERENCES "recording"("id") ON DELETE CASCADE,
 "recording_media_id" varchar(32) NOT NULL REFERENCES "recording_media"("id") ON DELETE CASCADE,
 "recording_transcript_run_id" varchar(32) NOT NULL REFERENCES "recording_transcript_run"("id") ON DELETE CASCADE,
 "recording_speaker_correction_id" varchar(32) NOT NULL REFERENCES "recording_speaker_correction"("id") ON DELETE CASCADE,
 "speaker_identity_id" varchar(32) NOT NULL REFERENCES "recording_speaker_identity"("id") ON DELETE CASCADE,
 "recording_blob_id" varchar(128) REFERENCES "recording_blob"("id") ON DELETE SET NULL,
 "passage_start_ms" bigint NOT NULL CHECK ("passage_start_ms" >= 0),
 "passage_end_ms" bigint NOT NULL CHECK ("passage_end_ms" - "passage_start_ms" BETWEEN 2000 AND 10000),
 "source_generation" bigint NOT NULL,
 "is_revoked" boolean NOT NULL DEFAULT false,
 "created_at" timestamptz NOT NULL,
 UNIQUE ("recording_speaker_correction_id", "passage_start_ms", "passage_end_ms")
);
CREATE INDEX "recording_correction_source" ON "recording_speaker_correction" ("recording_id", "source_generation");

-- Index removal preserves evidence identifiers for later raw-source reconciliation.
CREATE TABLE "recording_knowledge_document" (
 "recording_id" varchar(32) NOT NULL REFERENCES "recording"("id") ON DELETE CASCADE,
 "agent_id" varchar(32) NOT NULL REFERENCES "agent"("id") ON DELETE CASCADE,
 "agent_document_id" varchar(32) NOT NULL,
 PRIMARY KEY ("recording_id", "agent_document_id")
);
