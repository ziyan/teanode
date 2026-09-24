-- The conversation a schedule was made in. A schedule that answers in the
-- drawer takes its turn there, the way a goal's check-in does, so what it
-- says follows what was being talked about when it was set. Empty for one
-- made from the dashboard or the command line, which answers in the main
-- conversation.
ALTER TABLE "agent_schedule" ADD COLUMN "conversation_id" character varying(32) NOT NULL DEFAULT '';
