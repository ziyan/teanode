-- A person's agent, and the work it does.
--
-- The agent is one row per account: what the person told it about
-- themselves and how they want to be told what it did. A mailbox becomes a
-- source the agent may reach by a JSON column on the mailbox row, beside the
-- rules and the out-of-office setting, because it is edited the same way and
-- as rarely. Nothing about a mailbox that has not been granted is ever read
-- by a model.
CREATE TABLE "agent" (
    "id"                   character varying(32)    NOT NULL,
    "created_at"           timestamp with time zone NOT NULL,
    "modified_at"          timestamp with time zone NOT NULL,
    "user_id"              character varying(32)    NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    "name"                 character varying(64)    NOT NULL DEFAULT '',
    "enabled"              boolean                  NOT NULL DEFAULT false,
    "instructions"         text                     NOT NULL DEFAULT '',
    "language"             character varying(16)    NOT NULL DEFAULT '',
    "voice"                jsonb,
    "categories"           jsonb                    NOT NULL DEFAULT '[]',
    "notifications"        jsonb,
    "confirm"              jsonb                    NOT NULL DEFAULT '[]',
    "ask_model"            character varying(200)   NOT NULL DEFAULT '',
    "daily_tokens"         bigint                   NOT NULL DEFAULT 0,
    "operator_disabled_at" timestamp with time zone,
    PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "agent_user" ON "agent" ("user_id");

ALTER TABLE "mailbox" ADD COLUMN IF NOT EXISTS "agent" jsonb;

-- The queue. A job is claimed with FOR UPDATE SKIP LOCKED, so several
-- instances share one queue without a coordinator. A job that is queued or
-- running for the same agent, kind and subject is not queued again: the
-- second arrival of the same thread coalesces into the first.
CREATE TABLE "agent_job" (
    "id"          character varying(32)    NOT NULL,
    "created_at"  timestamp with time zone NOT NULL,
    "agent_id"    character varying(32)    NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,
    "mailbox_id"  character varying(32)    NOT NULL DEFAULT '',
    "kind"        character varying(16)    NOT NULL,
    "subject_id"  character varying(32)    NOT NULL DEFAULT '',
    "status"      character varying(16)    NOT NULL DEFAULT 'queued',
    "attempts"    integer                  NOT NULL DEFAULT 0,
    "not_before"  timestamp with time zone,
    "claimed_at"  timestamp with time zone,
    "claimed_by"  character varying(64)    NOT NULL DEFAULT '',
    "error"       text                     NOT NULL DEFAULT '',
    "finished_at" timestamp with time zone,
    PRIMARY KEY ("id")
);
CREATE INDEX "agent_job_claim" ON "agent_job" ("status", "not_before", "created_at");
CREATE INDEX "agent_job_agent" ON "agent_job" ("agent_id", "created_at");
CREATE INDEX "agent_job_open" ON "agent_job" ("agent_id", "kind", "subject_id") WHERE "status" IN ('queued', 'running');

-- Tokens, hourly, per agent, per source, per model and per kind of run, so
-- that the person's panel and the operator's view read the same rows and
-- cannot disagree. The values array is prompt, completion, cache read, cache
-- write, calls — see models.AgentUsagePromptTokens and the rest.
CREATE TABLE "agent_usage" (
    "backend_id" character varying(32)  NOT NULL,
    "agent_id"   character varying(32)  NOT NULL,
    "mailbox_id" character varying(32)  NOT NULL DEFAULT '',
    "model"      character varying(200) NOT NULL DEFAULT '',
    "kind"       character varying(16)  NOT NULL DEFAULT '',
    "interval"   bigint                 NOT NULL,
    "timestamp"  bigint                 NOT NULL,
    "values"     bigint[]               NOT NULL,
    PRIMARY KEY ("backend_id", "agent_id", "mailbox_id", "model", "kind", "interval", "timestamp")
);
CREATE INDEX "agent_usage_agent_time" ON "agent_usage" ("agent_id", "interval", "timestamp");
CREATE INDEX "agent_usage_time" ON "agent_usage" ("interval", "timestamp");

-- Where the person is and what they read in, learned from the browser or
-- the command line unless pinned. What every time the agent states is
-- rendered in, and what it writes in when the person has not chosen a
-- language.
ALTER TABLE "user" ADD COLUMN IF NOT EXISTS "timezone" character varying(64) NOT NULL DEFAULT '';
ALTER TABLE "user" ADD COLUMN IF NOT EXISTS "timezone_mode" character varying(8) NOT NULL DEFAULT 'auto';
ALTER TABLE "user" ADD COLUMN IF NOT EXISTS "timezone_seen_at" timestamp with time zone;
ALTER TABLE "user" ADD COLUMN IF NOT EXISTS "locale_seen" character varying(16) NOT NULL DEFAULT '';

-- The roles seeded before agents existed gain the agent's permissions:
-- everyone may have one, and operators may audit them. Once, here, so
-- that an operator who later takes one away is not overruled at every
-- start.
INSERT INTO "role_permission" ("role_id", "permission_key")
SELECT "id", 'agent:use' FROM "role" WHERE lower("name") IN ('administrator', 'operator', 'member')
ON CONFLICT DO NOTHING;
INSERT INTO "role_permission" ("role_id", "permission_key")
SELECT "id", 'agent:audit' FROM "role" WHERE lower("name") IN ('administrator', 'operator')
ON CONFLICT DO NOTHING;
