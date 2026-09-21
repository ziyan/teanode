-- When the goal on a conversation was set.
--
-- The drawer's goal panel says since when the agent has been working
-- toward it, which the row could not say: the four columns the goal
-- began with carried the text, the state, the last note and the next
-- look, and the row's own modified time moves with every note.
ALTER TABLE "agent_conversation" ADD COLUMN "goal_set_at" timestamp with time zone;
