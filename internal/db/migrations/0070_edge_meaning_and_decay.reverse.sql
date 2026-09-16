DROP INDEX IF EXISTS "agent_edge_from";
ALTER TABLE "agent_edge" DROP COLUMN IF EXISTS "used_at";
ALTER TABLE "agent_edge" DROP COLUMN IF EXISTS "happened_at";
ALTER TABLE "agent_edge" DROP COLUMN IF EXISTS "note";
