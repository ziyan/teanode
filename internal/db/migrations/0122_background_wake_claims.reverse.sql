ALTER TABLE "agent_conversation" DROP COLUMN IF EXISTS "background_wake_count";
ALTER TABLE "agent_background_work" DROP COLUMN IF EXISTS "wake_claimed_at";
