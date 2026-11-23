-- One switch for what the agent suggests unasked: the ideas it offers and
-- its checks of what it remembers were two switches for one choice, whether
-- the agent may bring something up on its own. A person who had switched
-- either off keeps the agent quiet about both.
ALTER TABLE "agent" ADD COLUMN "is_suggestions_enabled" boolean NOT NULL DEFAULT true;
UPDATE "agent" SET "is_suggestions_enabled" = "is_ideas_enabled" AND "is_memory_check_enabled";
ALTER TABLE "agent" DROP COLUMN "is_ideas_enabled", DROP COLUMN "is_memory_check_enabled";
