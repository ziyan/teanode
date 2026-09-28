UPDATE "agent_job" SET "subject_id" = 'tip:asked' WHERE "kind" = 'speak_first' AND "subject_id" = 'idea:asked';
UPDATE "agent_job" SET "subject_id" = 'tip' WHERE "kind" = 'speak_first' AND "subject_id" = 'idea';

ALTER TABLE "agent" RENAME COLUMN "is_ideas_enabled" TO "is_tips_enabled";

CREATE TABLE "agent_tip" (
    "id"              character varying(32)    NOT NULL PRIMARY KEY,
    "agent_id"        character varying(32)    NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,
    "tip_key"         character varying(64)    NOT NULL,
    "given_at"        timestamp with time zone NOT NULL,
    "conversation_id" character varying(32)    NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX "agent_tip_key" ON "agent_tip" ("agent_id", "tip_key");
INSERT INTO "agent_tip" ("id", "agent_id", "tip_key", "given_at")
SELECT "id", "agent_id", "idea_key", "shown_at" FROM "agent_idea" WHERE "idea_kind" = 'catalog' AND "shown_at" IS NOT NULL;

DROP TABLE IF EXISTS "agent_idea";
