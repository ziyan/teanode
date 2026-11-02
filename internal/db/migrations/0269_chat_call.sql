-- A call in a chat: who started it, when somebody else first joined, when it
-- ended and how. At most one call is going in a chat at a time ("ended_at" is
-- null while it goes), so a second person who starts one joins it instead.
--
-- "end_reason" is '' while the call goes, then 'ended' when the last person
-- left, 'missed' when nobody joined before the ringing stopped, 'declined'
-- when everybody rung said no, and 'failed' when the server stopped under it.
-- "instance" is the server instance whose media server holds the call.
--
-- The transcript is asked for during the call; "transcript_state" is '' when
-- it was not, then 'waiting' until it is written ('done') or given up
-- ('failed'). The summary is posted into the chat as a message.
CREATE TABLE "chat_call" (
    "id"                             character varying(32)  NOT NULL PRIMARY KEY,
    "chat_id"                        character varying(32)  NOT NULL REFERENCES "chat" ("id") ON DELETE CASCADE,
    "started_by_user_id"             character varying(32)  REFERENCES "user" ("id") ON DELETE SET NULL,
    "started_at"                     timestamp with time zone NOT NULL,
    "answered_at"                    timestamp with time zone,
    "ended_at"                       timestamp with time zone,
    "end_reason"                     character varying(16)  NOT NULL DEFAULT ''
        CHECK ("end_reason" IN ('', 'ended', 'missed', 'declined', 'failed')),
    "instance"                       character varying(64)  NOT NULL DEFAULT '',
    "transcription_asked_by_user_id" character varying(32)  REFERENCES "user" ("id") ON DELETE SET NULL,
    "transcript_state"               character varying(16)  NOT NULL DEFAULT ''
        CHECK ("transcript_state" IN ('', 'waiting', 'done', 'failed')),
    "transcript_tried_at"            timestamp with time zone,
    "summary_message_id"             character varying(32)
);

CREATE UNIQUE INDEX "chat_call_going" ON "chat_call" ("chat_id") WHERE "ended_at" IS NULL;
CREATE INDEX "chat_call_chat" ON "chat_call" ("chat_id", "started_at" DESC);
CREATE INDEX "chat_call_transcript_waiting" ON "chat_call" ("ended_at") WHERE "transcript_state" = 'waiting';

-- Each member of the chat when the call started, and anyone who joined later:
-- whether they were rung and what came of it ('ringing', then 'answered',
-- 'declined' or 'missed'; 'none' for the one who started it), when they were
-- on the call, and their answer to a transcript being asked for ('', 'yes' or
-- 'no'). Only the voices of those who said yes are transcribed.
CREATE TABLE "chat_call_member" (
    "call_id"               character varying(32)  NOT NULL REFERENCES "chat_call" ("id") ON DELETE CASCADE,
    "user_id"               character varying(32)  NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    "ring_state"            character varying(16)  NOT NULL DEFAULT 'ringing'
        CHECK ("ring_state" IN ('none', 'ringing', 'answered', 'declined', 'missed')),
    "joined_at"             timestamp with time zone,
    "left_at"               timestamp with time zone,
    "is_video_on"           boolean NOT NULL DEFAULT false,
    "transcription_consent" character varying(8)   NOT NULL DEFAULT ''
        CHECK ("transcription_consent" IN ('', 'yes', 'no')),
    PRIMARY KEY ("call_id", "user_id")
);

CREATE INDEX "chat_call_member_user" ON "chat_call_member" ("user_id");

-- What was said on a transcribed call, a line at a time under its speaker,
-- in the order it was said.
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

-- The call's line in the chat is a message with no words that names the
-- call; a client draws it from the call.
ALTER TABLE "chat_message"
    ADD COLUMN "call_id" character varying(32) REFERENCES "chat_call" ("id") ON DELETE SET NULL;

-- A phone's second registration with the push relay, for the pushes that
-- ring it (PushKit), sealed by the relay like push_registration; empty for a
-- phone that has none.
ALTER TABLE "agent_phone"
    ADD COLUMN "voip_push_registration" text NOT NULL DEFAULT '';
