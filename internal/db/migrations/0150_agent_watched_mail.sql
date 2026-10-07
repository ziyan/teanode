-- The messages the watch has looked at in a mailbox TeaNode does not host,
-- read through a skill: one row per message, so the next look, which
-- overlaps this one, does not sort a message twice. Gone with the agent.
CREATE TABLE "agent_watched_mail" (
    "agent_id"           character varying(32)    NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,
    "skill_name"         character varying(100)   NOT NULL,
    "watched_message_id" character varying(200)   NOT NULL,
    "watched_message_at" timestamp with time zone NOT NULL,

    -- What the sorting said: none, soon or now.
    "alert_signal"       character varying(10)    NOT NULL DEFAULT 'none',
    "looked_at"          timestamp with time zone NOT NULL,
    PRIMARY KEY ("agent_id", "skill_name", "watched_message_id")
);
CREATE INDEX "agent_watched_mail_newest" ON "agent_watched_mail" ("agent_id", "skill_name", "watched_message_at" DESC);

-- A candidate about a watched message carries what the alert job reads,
-- since the message is not stored: the skill, the message's id, sender,
-- subject and date, the sorting's category, and the message as the
-- sorting saw it.
ALTER TABLE "agent_alert_candidate"
    ADD COLUMN "watched_skill_name"    character varying(100)   NOT NULL DEFAULT '',
    ADD COLUMN "watched_message_id"    character varying(200)   NOT NULL DEFAULT '',
    ADD COLUMN "watched_sender"        text                     NOT NULL DEFAULT '',
    ADD COLUMN "watched_subject"       text                     NOT NULL DEFAULT '',
    ADD COLUMN "watched_mail_category" character varying(40)    NOT NULL DEFAULT '',
    ADD COLUMN "watched_message_at"    timestamp with time zone,
    ADD COLUMN "watched_message_text"  text                     NOT NULL DEFAULT '';
