ALTER TABLE "agent"
    ADD COLUMN "is_ideas_enabled"        boolean NOT NULL DEFAULT true,
    ADD COLUMN "is_memory_check_enabled" boolean NOT NULL DEFAULT true;
UPDATE "agent" SET "is_ideas_enabled" = "is_suggestions_enabled", "is_memory_check_enabled" = "is_suggestions_enabled";
ALTER TABLE "agent" DROP COLUMN "is_suggestions_enabled";
