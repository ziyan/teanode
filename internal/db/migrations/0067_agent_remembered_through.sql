-- How far through a conversation the agent has filed what it learned.
--
-- The identifier of the last message a remember run read, not the time it
-- ran: a run that dies re-reads from where it was rather than skipping
-- whatever arrived while it was working. Written in the same transaction
-- as the facts it wrote, so the two cannot disagree.
--
-- Empty means nothing has been filed yet, which is true of every
-- conversation that existed before this.
ALTER TABLE "agent_conversation" ADD COLUMN "remembered_through" character varying(32) NOT NULL DEFAULT '';

-- When the last remember run finished, so that a conversation still being
-- typed into is left alone and one nothing has been said in is not asked
-- about every minute.
ALTER TABLE "agent_conversation" ADD COLUMN "remembered_at" timestamp with time zone;

CREATE INDEX "agent_conversation_remember" ON "agent_conversation" ("kind", "last_at")
    WHERE "kind" IN ('main', 'named');
