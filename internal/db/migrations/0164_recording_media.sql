-- Originals remain owned and referenced while blob cleanup is pending.
CREATE TABLE "recording" (
    "id" varchar(32) PRIMARY KEY,
    "user_id" varchar(32) REFERENCES "user"("id") ON DELETE SET NULL,
    "created_at" timestamptz NOT NULL,
    "updated_at" timestamptz NOT NULL,
    "client_recording_id" varchar(128) NOT NULL,
    "recording_title" varchar(255) NOT NULL,
    "recording_origin" varchar(32) NOT NULL,
    "recording_capture_state" varchar(32) NOT NULL,
    "timeline_duration_ms" bigint NOT NULL DEFAULT 0 CHECK ("timeline_duration_ms" >= 0),
    "capture_gaps" jsonb NOT NULL DEFAULT '[]',
    "source_generation" bigint NOT NULL DEFAULT 1,
    "deleted_at" timestamptz,
    "cleanup_completed_at" timestamptz,
    UNIQUE ("user_id", "client_recording_id")
);
CREATE TABLE "recording_media" (
    "id" varchar(32) PRIMARY KEY,
    "recording_id" varchar(32) NOT NULL REFERENCES "recording"("id") ON DELETE CASCADE,
    "created_at" timestamptz NOT NULL,
    "client_media_id" varchar(128) NOT NULL,
    "original_filename" varchar(255) NOT NULL,
    "original_content_type" varchar(128) NOT NULL,
    "original_size_bytes" bigint NOT NULL CHECK ("original_size_bytes" > 0),
    "original_sha256" varchar(64) NOT NULL,
    "timeline_start_ms" bigint NOT NULL CHECK ("timeline_start_ms" >= 0),
    "media_duration_ms" bigint NOT NULL CHECK ("media_duration_ms" >= 0),
    "part_count" integer NOT NULL CHECK ("part_count" > 0),
    "recording_upload_state" varchar(32) NOT NULL,
    "verified_at" timestamptz,
    "cleanup_next_part_index" integer NOT NULL DEFAULT 0 CHECK ("cleanup_next_part_index" >= 0 AND "cleanup_next_part_index" <= "part_count"),
    UNIQUE ("recording_id", "client_media_id")
);
CREATE TABLE "recording_media_part" (
    "recording_media_id" varchar(32) NOT NULL REFERENCES "recording_media"("id") ON DELETE CASCADE,
    "part_index" integer NOT NULL CHECK ("part_index" >= 0),
    "part_size_bytes" bigint NOT NULL CHECK ("part_size_bytes" > 0),
    "part_sha256" varchar(64) NOT NULL,
    "stored_at" timestamptz NOT NULL,
    PRIMARY KEY ("recording_media_id", "part_index")
);
CREATE INDEX "recording_owner_created" ON "recording" ("user_id", "created_at" DESC);
CREATE INDEX "recording_cleanup" ON "recording" ("deleted_at") WHERE "deleted_at" IS NOT NULL;
