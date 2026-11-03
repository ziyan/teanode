-- A transcribed call is a recording, owned by whoever turned transcription
-- on: "chat_call_id" names the call it was made from, and
-- "chat_call"."recording_id" the recording a call became.
ALTER TABLE "recording"
    ADD COLUMN "chat_call_id" character varying(32) REFERENCES "chat_call" ("id") ON DELETE SET NULL;
CREATE UNIQUE INDEX "recording_chat_call" ON "recording" ("chat_call_id") WHERE "chat_call_id" IS NOT NULL;

ALTER TABLE "chat_call"
    ADD COLUMN "recording_id" character varying(32) REFERENCES "recording" ("id") ON DELETE SET NULL;

-- An original that is one person's own microphone names them, so its
-- transcript needs no guessing who spoke: "speaker_user_id" is the person
-- (kept as it was when they are gone) and "speaker_name" their name then.
-- Both are empty for an original of several voices.
ALTER TABLE "recording_media"
    ADD COLUMN "speaker_user_id" character varying(32) NOT NULL DEFAULT '',
    ADD COLUMN "speaker_name" character varying(100) NOT NULL DEFAULT '';

-- The transcript is the recording's.
DROP TABLE "chat_call_transcript_segment";
