-- What the agent keeps between conversations, and what it does on its own
-- time.
--
-- A memory is a durable fact about the person, addressed to the kinds of
-- run that should read it. A correction is something the person did that
-- contradicted what the agent decided, kept for a while and shown to the
-- next run as an example. A schedule is a prompt run at times the person
-- chose, in their own zone, delivered by mail or into their conversation.
-- A todo is a conversation's own task list, so a long piece of work does
-- not lose its place.
CREATE TABLE "agent_memory" (
    "id"          character varying(32)    NOT NULL PRIMARY KEY,
    "created_at"  timestamp with time zone NOT NULL,
    "modified_at" timestamp with time zone NOT NULL,
    "agent_id"    character varying(32)    NOT NULL,
    "title"       character varying(200)   NOT NULL,
    "content"     text                     NOT NULL,
    "tags"        jsonb                    NOT NULL DEFAULT '[]',
    "applies_to"  jsonb                    NOT NULL DEFAULT '[]',
    "pinned"      boolean                  NOT NULL DEFAULT false,
    "used_at"     timestamp with time zone
);
CREATE INDEX "agent_memory_agent_used" ON "agent_memory" ("agent_id", "pinned", "used_at" DESC);

CREATE TABLE "agent_feedback" (
    "id"         character varying(32)    NOT NULL PRIMARY KEY,
    "created_at" timestamp with time zone NOT NULL,
    "agent_id"   character varying(32)    NOT NULL,
    "mailbox_id" character varying(32)    NOT NULL DEFAULT '',
    "kind"       character varying(32)    NOT NULL,
    "mail_id"    character varying(32)    NOT NULL DEFAULT '',
    "said"       text                     NOT NULL
);
CREATE INDEX "agent_feedback_agent_created" ON "agent_feedback" ("agent_id", "created_at" DESC);

CREATE TABLE "agent_schedule" (
    "id"          character varying(32)    NOT NULL PRIMARY KEY,
    "created_at"  timestamp with time zone NOT NULL,
    "modified_at" timestamp with time zone NOT NULL,
    "agent_id"    character varying(32)    NOT NULL,
    "name"        character varying(200)   NOT NULL,
    "cron"        character varying(100)   NOT NULL,
    "prompt"      text                     NOT NULL,
    "deliver"     character varying(16)    NOT NULL DEFAULT 'drawer',
    "enabled"     boolean                  NOT NULL DEFAULT true,
    "last_run_at" timestamp with time zone,
    "next_run_at" timestamp with time zone
);
CREATE INDEX "agent_schedule_due" ON "agent_schedule" ("enabled", "next_run_at");

CREATE TABLE "agent_todo" (
    "id"              character varying(32)    NOT NULL PRIMARY KEY,
    "created_at"      timestamp with time zone NOT NULL,
    "conversation_id" character varying(32)    NOT NULL,
    "text"            text                     NOT NULL,
    "done_at"         timestamp with time zone
);
CREATE INDEX "agent_todo_conversation" ON "agent_todo" ("conversation_id", "created_at");
