CREATE TABLE "chat_call_transcript_segment" (
    "id"         character varying(32)  NOT NULL PRIMARY KEY,
    "call_id"    character varying(32)  NOT NULL REFERENCES "chat_call" ("id") ON DELETE CASCADE,
    "user_id"    character varying(32)  REFERENCES "user" ("id") ON DELETE SET NULL,
    "speaker_name" character varying(100) NOT NULL DEFAULT '',
    "start_ms"   bigint NOT NULL,
    "end_ms"     bigint NOT NULL,
    "text"       text NOT NULL
);
CREATE INDEX "chat_call_transcript_segment_call" ON "chat_call_transcript_segment" ("call_id", "start_ms");
ALTER TABLE "recording_media" DROP COLUMN "speaker_name", DROP COLUMN "speaker_user_id";
ALTER TABLE "chat_call" DROP COLUMN "recording_id";
DROP INDEX "recording_chat_call";
ALTER TABLE "recording" DROP COLUMN "chat_call_id";
