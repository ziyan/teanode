DROP INDEX IF EXISTS "agent_conversation_remember";
ALTER TABLE "agent_conversation" DROP COLUMN IF EXISTS "remembered_at";
ALTER TABLE "agent_conversation" DROP COLUMN IF EXISTS "remembered_through";
