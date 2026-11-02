-- A voice message is a chat message that is one recording, with its words
-- written under it a few seconds after it is posted.
--
-- The length and the waveform are facts of the recording, so they live on the
-- file's row and ride along in the copy of the file that the message keeps.
-- "audio_waveform" is 48 levels, 0 to 255 each, in base64. Both are zero and
-- empty on every file that is not a voice message.
--
-- "transcript_state" is '' on a message that is not a voice message, then
-- 'waiting' until the words are written ('done'), the attempt is given up
-- ('failed'), or there is nothing to pay for them ('off'). "transcript_tried_at"
-- is when a server last claimed the row, so that one server works on it.
ALTER TABLE "agent_attachment"
    ADD COLUMN "audio_duration_ms" bigint NOT NULL DEFAULT 0,
    ADD COLUMN "audio_waveform" varchar(128) NOT NULL DEFAULT '';

ALTER TABLE "chat_message"
    ADD COLUMN "is_voice_message" boolean NOT NULL DEFAULT false,
    ADD COLUMN "transcript_text" text NOT NULL DEFAULT '',
    ADD COLUMN "transcript_state" varchar(16) NOT NULL DEFAULT ''
        CHECK ("transcript_state" IN ('', 'waiting', 'done', 'failed', 'off')),
    ADD COLUMN "transcript_tried_at" timestamptz;

-- The messages still to be transcribed, oldest first, for the sweep.
CREATE INDEX "chat_message_transcript_waiting" ON "chat_message" ("created_at") WHERE "transcript_state" = 'waiting';
