CREATE TABLE "recording_blob" (
 "id" varchar(128) PRIMARY KEY,
 "recording_id" varchar(32) NOT NULL REFERENCES "recording"("id") ON DELETE RESTRICT,
 "recording_media_id" varchar(32) NOT NULL REFERENCES "recording_media"("id") ON DELETE RESTRICT,
 "created_at" timestamptz NOT NULL,
 "recording_blob_purpose" varchar(32) NOT NULL,
 "blob_size_bytes" bigint NOT NULL CHECK ("blob_size_bytes" > 0),
 "blob_sha256" varchar(64) NOT NULL,
 "is_stored_verified" boolean NOT NULL DEFAULT false
);
CREATE TABLE "recording_transcript_run" (
 "id" varchar(32) PRIMARY KEY,
 "recording_id" varchar(32) NOT NULL REFERENCES "recording"("id") ON DELETE CASCADE,
 "recording_media_id" varchar(32) NOT NULL REFERENCES "recording_media"("id") ON DELETE CASCADE,
 "agent_id" varchar(32) REFERENCES "agent"("id") ON DELETE SET NULL,
 "created_at" timestamptz NOT NULL,
 "updated_at" timestamptz NOT NULL,
 "client_run_id" varchar(128) NOT NULL,
 "version_number" integer NOT NULL CHECK ("version_number" > 0),
 "recording_processing_state" varchar(32) NOT NULL,
 "provider_model_name" varchar(255) NOT NULL,
 "original_sha256" varchar(64) NOT NULL,
 "derived_duration_ms" bigint NOT NULL DEFAULT 0 CHECK ("derived_duration_ms" >= 0),
 "processing_error_message" text NOT NULL DEFAULT '',
 "is_reprocessing_requested" boolean NOT NULL DEFAULT false,
 "completed_at" timestamptz,
 UNIQUE ("recording_media_id","version_number"),
 UNIQUE ("recording_media_id","client_run_id")
);
CREATE TABLE "recording_audio_chunk" (
 "id" varchar(32) PRIMARY KEY,
 "recording_transcript_run_id" varchar(32) NOT NULL REFERENCES "recording_transcript_run"("id") ON DELETE CASCADE,
 "recording_blob_id" varchar(128) NOT NULL REFERENCES "recording_blob"("id") ON DELETE CASCADE,
 "chunk_index" integer NOT NULL CHECK ("chunk_index" >= 0),
 "timeline_start_ms" bigint NOT NULL CHECK ("timeline_start_ms" >= 0),
 "audio_duration_ms" bigint NOT NULL CHECK ("audio_duration_ms" > 0),
 UNIQUE ("recording_transcript_run_id","chunk_index")
);
CREATE TABLE "recording_transcription_attempt" (
 "id" varchar(32) PRIMARY KEY,
 "recording_transcript_run_id" varchar(32) NOT NULL REFERENCES "recording_transcript_run"("id") ON DELETE CASCADE,
 "recording_audio_chunk_id" varchar(32) NOT NULL REFERENCES "recording_audio_chunk"("id") ON DELETE CASCADE,
 "attempt_index" integer NOT NULL CHECK ("attempt_index" > 0),
 "created_at" timestamptz NOT NULL,
 "submitted_at" timestamptz,
 "completed_at" timestamptz,
 "recording_attempt_state" varchar(32) NOT NULL,
 "provider_model_name" varchar(255) NOT NULL,
 "attempt_error_message" text NOT NULL DEFAULT '',
 "input_audio_tokens" bigint NOT NULL DEFAULT 0 CHECK ("input_audio_tokens" >= 0),
 "input_text_tokens" bigint NOT NULL DEFAULT 0 CHECK ("input_text_tokens" >= 0),
 "prompt_tokens" bigint NOT NULL DEFAULT 0 CHECK ("prompt_tokens" >= 0),
 "completion_tokens" bigint NOT NULL DEFAULT 0 CHECK ("completion_tokens" >= 0),
 "provider_audio_duration_ms" bigint NOT NULL DEFAULT 0 CHECK ("provider_audio_duration_ms" >= 0),
 "has_provider_usage" boolean NOT NULL DEFAULT false,
 "estimated_cost_amount" double precision CHECK ("estimated_cost_amount" >= 0),
 "actual_cost_amount" double precision CHECK ("actual_cost_amount" >= 0),
 "currency_code" varchar(3) NOT NULL,
 "provider_usage_json" jsonb NOT NULL DEFAULT '{}',
 "provider_response_json" jsonb NOT NULL DEFAULT '{}',
 "reference_blob_ids" jsonb NOT NULL DEFAULT '[]',
 UNIQUE ("recording_audio_chunk_id","attempt_index")
);
CREATE TABLE "recording_transcript_segment" (
 "id" varchar(32) PRIMARY KEY,
 "recording_transcript_run_id" varchar(32) NOT NULL REFERENCES "recording_transcript_run"("id") ON DELETE CASCADE,
 "recording_transcription_attempt_id" varchar(32) NOT NULL REFERENCES "recording_transcription_attempt"("id") ON DELETE CASCADE,
 "local_speaker_id" varchar(255) NOT NULL,
 "segment_start_ms" bigint NOT NULL CHECK ("segment_start_ms" >= 0),
 "segment_end_ms" bigint NOT NULL CHECK ("segment_end_ms" >= "segment_start_ms"),
 "transcript_text" text NOT NULL
);
CREATE INDEX "recording_transcript_source" ON "recording_transcript_run" ("recording_id","created_at");
CREATE INDEX "recording_transcript_timeline" ON "recording_transcript_segment" ("recording_transcript_run_id","segment_start_ms","id");
CREATE INDEX "recording_blob_source" ON "recording_blob" ("recording_id","id");
