-- Work the agent started and did not wait for: a survey or a subagent run
-- by a queued job, whose subject is the row. The job carries only an id, so
-- what was asked, where to wake when it finishes, and what came of it are
-- kept here, where the API and the command line read them too. Gone with
-- the agent.
CREATE TABLE "agent_background_work" (
    "id"                character varying(32)    NOT NULL PRIMARY KEY,
    "agent_id"          character varying(32)    NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,

    -- The conversation to wake when it finishes; empty when it was
    -- started from the API or the command line, which wakes nothing.
    "conversation_id"   character varying(32)    NOT NULL DEFAULT '',

    -- survey or subagent.
    "work_kind"         character varying(20)    NOT NULL,
    "title"             text                     NOT NULL DEFAULT '',

    -- What was asked: a survey's question and scope, a subagent's prompt
    -- and the tools it may use.
    "work_request"      jsonb                    NOT NULL DEFAULT '{}',

    -- Whether the turn that started it had the person present, without
    -- which it wakes nothing.
    "is_person_present" boolean                  NOT NULL DEFAULT false,

    -- queued, running, done, failed or stopped.
    "work_status"       character varying(20)    NOT NULL DEFAULT 'queued',

    -- The report or the subagent's answer, the runs it made, and why it
    -- failed when it did.
    "result_text"       text                     NOT NULL DEFAULT '',
    "run_ids"           jsonb                    NOT NULL DEFAULT '[]',
    "error_message"     text                     NOT NULL DEFAULT '',

    "created_at"        timestamp with time zone NOT NULL,
    "started_at"        timestamp with time zone,
    "finished_at"       timestamp with time zone,
    "woken_at"          timestamp with time zone
);
CREATE INDEX "agent_background_work_agent" ON "agent_background_work" ("agent_id", "created_at");
CREATE INDEX "agent_background_work_open" ON "agent_background_work" ("work_status", "created_at");
