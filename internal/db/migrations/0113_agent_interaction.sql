-- A question or an approval the agent put to the person, kept until it is
-- answered, however long that takes. The turn that raised it waits a few
-- minutes and then ends; the card stays, and a late answer starts a new
-- turn with it.
CREATE TABLE "agent_interaction" (
    "id"                  character varying(32)    NOT NULL PRIMARY KEY,
    "agent_id"            character varying(32)    NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,
    "conversation_id"     character varying(32)    NOT NULL,
    "run_id"              character varying(32)    NOT NULL,
    "call_id"             character varying(200)   NOT NULL,
    -- question or approval.
    "interaction_kind"    character varying(20)    NOT NULL,
    "tool_name"           character varying(200)   NOT NULL DEFAULT '',
    "tool_arguments"      text                     NOT NULL DEFAULT '',
    -- The question, or the approval card's line.
    "interaction_text"    text                     NOT NULL DEFAULT '',
    "interaction_choices" text[]                   NOT NULL DEFAULT '{}',
    "tool_risk"           character varying(20)    NOT NULL DEFAULT '',
    "created_at"          timestamp with time zone NOT NULL,
    "resolved_at"         timestamp with time zone,
    -- The answer; approved or declined; or stopped, for a turn stopped
    -- while it waited.
    "interaction_answer"  text                     NOT NULL DEFAULT ''
);
CREATE INDEX "agent_interaction_open" ON "agent_interaction" ("conversation_id", "created_at") WHERE "resolved_at" IS NULL;
CREATE INDEX "agent_interaction_call" ON "agent_interaction" ("agent_id", "call_id");
