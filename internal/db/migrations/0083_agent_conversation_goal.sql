-- A goal on a conversation: a sentence the person set, or asked the agent
-- to set, that stays until it is met or cleared. While one is set the
-- agent takes turns in the conversation on its own, so the row has to say
-- what the goal is, where it stands, what the agent last said about it,
-- and when it looks again.
--
-- Empty text means no goal, and the state is empty with it; the three
-- states are working, waiting (the agent needs the person) and met. Text
-- rather than an enumeration, because the states of this are still being
-- learned and an enumeration costs a migration to add one.
ALTER TABLE "agent_conversation"
    ADD COLUMN "goal" text NOT NULL DEFAULT '',
    ADD COLUMN "goal_state" text NOT NULL DEFAULT '',
    ADD COLUMN "goal_note" text NOT NULL DEFAULT '',
    ADD COLUMN "goal_next_at" timestamp with time zone;

-- The sweep that queues a turn asks one question every five seconds:
-- which conversations are working and due. Partial, so the index holds
-- the handful of conversations with a goal running rather than every
-- conversation on the server.
CREATE INDEX "agent_conversation_goal_due"
    ON "agent_conversation" ("goal_next_at")
    WHERE "goal_state" = 'working';
