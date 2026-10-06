DROP TABLE IF EXISTS "agent_goal_artifact";
DROP TABLE IF EXISTS "agent_goal_activity";
-- A build without goal conversations reads the kind as unknown; as named
-- conversations they stay readable, with their goal on them.
UPDATE "agent_conversation" SET "kind" = 'named' WHERE "kind" = 'goal';
DROP INDEX IF EXISTS "agent_conversation_goal_to_surface";
ALTER TABLE "agent_conversation" DROP COLUMN IF EXISTS "goal_surfaced_at";
ALTER TABLE "agent_conversation" DROP COLUMN IF EXISTS "goal_title";
