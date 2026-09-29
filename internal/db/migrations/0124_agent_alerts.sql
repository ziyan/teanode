-- What the agent told the person unasked: the words, what they were about,
-- and where they were said. Read to keep to a few a day and to say nothing
-- twice, and written in the same transaction as the message in the main
-- conversation, so a job run again after a restart finds it and does not
-- tell them again. Gone with the agent.
CREATE TABLE "agent_alert" (
    "id"              character varying(32)    NOT NULL PRIMARY KEY,
    "agent_id"        character varying(32)    NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,

    -- The sender and what it is about, in the same words each time the
    -- same thing comes up.
    "subject_key"     text                     NOT NULL DEFAULT '',
    "alert_text"      text                     NOT NULL DEFAULT '',

    -- Said at night because it could not wait for the morning.
    "is_urgent"       boolean                  NOT NULL DEFAULT false,

    -- The candidates it covered, and the message it is in the conversation.
    "candidate_ids"   jsonb                    NOT NULL DEFAULT '[]',
    "conversation_id" character varying(32)    NOT NULL DEFAULT '',
    "message_id"      character varying(32)    NOT NULL DEFAULT '',

    "sent_at"         timestamp with time zone NOT NULL
);
CREATE INDEX "agent_alert_sent" ON "agent_alert" ("agent_id", "sent_at" DESC);
