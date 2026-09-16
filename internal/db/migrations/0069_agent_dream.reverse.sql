ALTER TABLE "agent_node" DROP COLUMN IF EXISTS "consolidated_at";
ALTER TABLE "agent" DROP COLUMN IF EXISTS "dreamed_at";
ALTER TABLE "agent" DROP COLUMN IF EXISTS "dream_until";
ALTER TABLE "agent" DROP COLUMN IF EXISTS "dream_from";
DROP TABLE IF EXISTS "agent_dream";
