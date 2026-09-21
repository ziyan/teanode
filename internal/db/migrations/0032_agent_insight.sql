-- What the agent worked out about a message, and the record of it working.
--
-- An insight is one row per message per mailbox: the category, the priority,
-- whether a reply is needed, a one-line summary. Two people who received the
-- same message each get their own, because their agents were told different
-- things. Rules read it in their second phase; the list shows it as chips.
CREATE TABLE "mail_insight" (
    "mail_id"        character varying(32)    NOT NULL REFERENCES "mail" ("id") ON DELETE CASCADE,
    "mailbox_id"     character varying(32)    NOT NULL REFERENCES "mailbox" ("id") ON DELETE CASCADE,
    "agent_id"       character varying(32)    NOT NULL,
    "category"       character varying(64)    NOT NULL DEFAULT '',
    "priority"       character varying(16)    NOT NULL DEFAULT 'normal',
    "needs_reply"    boolean                  NOT NULL DEFAULT false,
    "research_asked" boolean                  NOT NULL DEFAULT false,
    "summary"        text                     NOT NULL DEFAULT '',
    "action_items"   jsonb                    NOT NULL DEFAULT '[]',
    "notes"          text                     NOT NULL DEFAULT '',
    "notes_run_id"   character varying(32)    NOT NULL DEFAULT '',
    "model"          character varying(200)   NOT NULL DEFAULT '',
    "run_id"         character varying(32)    NOT NULL DEFAULT '',
    "created_at"     timestamp with time zone NOT NULL,
    PRIMARY KEY ("mail_id", "mailbox_id")
);
CREATE INDEX "mail_insight_mailbox_priority" ON "mail_insight" ("mailbox_id", "priority");
CREATE INDEX "mail_insight_mailbox_category" ON "mail_insight" ("mailbox_id", "category");

-- Every run leaves a transcript, and every conversation a person has with
-- their agent is stored the same way: a conversation of a kind, and its
-- messages. A run's conversation names the job it was.
CREATE TABLE "agent_conversation" (
    "id"                character varying(32)    NOT NULL,
    "created_at"        timestamp with time zone NOT NULL,
    "modified_at"       timestamp with time zone NOT NULL,
    "agent_id"          character varying(32)    NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,
    "mailbox_id"        character varying(32)    NOT NULL DEFAULT '',
    "kind"              character varying(16)    NOT NULL,
    "title"             character varying(200)   NOT NULL DEFAULT '',
    "job_id"            character varying(32)    NOT NULL DEFAULT '',
    "job_kind"          character varying(16)    NOT NULL DEFAULT '',
    "subject_id"        character varying(32)    NOT NULL DEFAULT '',
    "surface"           character varying(16)    NOT NULL DEFAULT '',
    "archived_at"       timestamp with time zone,
    "last_at"           timestamp with time zone NOT NULL,
    "compacted_through" character varying(32)    NOT NULL DEFAULT '',
    PRIMARY KEY ("id")
);
CREATE INDEX "agent_conversation_agent" ON "agent_conversation" ("agent_id", "kind", "last_at");
CREATE INDEX "agent_conversation_subject" ON "agent_conversation" ("agent_id", "subject_id");

CREATE TABLE "agent_message" (
    "id"              character varying(32)    NOT NULL,
    "created_at"      timestamp with time zone NOT NULL,
    "conversation_id" character varying(32)    NOT NULL REFERENCES "agent_conversation" ("id") ON DELETE CASCADE,
    "role"            character varying(16)    NOT NULL,
    "content"         text                     NOT NULL DEFAULT '',
    "tool_calls"      jsonb,
    "tool_call_id"    character varying(64)    NOT NULL DEFAULT '',
    "name"            character varying(128)   NOT NULL DEFAULT '',
    "usage"           jsonb,
    PRIMARY KEY ("id")
);
CREATE INDEX "agent_message_conversation" ON "agent_message" ("conversation_id", "created_at");
