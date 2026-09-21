DROP INDEX IF EXISTS "agent_conversation_goal_due";
ALTER TABLE "agent_conversation" DROP COLUMN IF EXISTS "goal_next_at";
ALTER TABLE "agent_conversation" DROP COLUMN IF EXISTS "goal_note";
ALTER TABLE "agent_conversation" DROP COLUMN IF EXISTS "goal_state";
ALTER TABLE "agent_conversation" DROP COLUMN IF EXISTS "goal";
