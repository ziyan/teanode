-- When a conversation was last described: a title and summary are written
-- once it has been quiet for a few minutes and something was said since
-- the last description, not after every turn.
ALTER TABLE "agent_conversation" ADD COLUMN "described_at" timestamp with time zone;
