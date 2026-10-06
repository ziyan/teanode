-- The conversation a goal was asked for in, so the goal's page can link to
-- it. Nothing when it was started from the Goals tab or the command line,
-- or when that conversation is deleted.
ALTER TABLE "agent_conversation"
    ADD COLUMN "goal_origin_conversation_id" character varying(32)
    REFERENCES "agent_conversation" ("id") ON DELETE SET NULL;
