ALTER TABLE "agent_alert_candidate" DROP COLUMN IF EXISTS "watched_item_url";
ALTER TABLE "agent_alert_candidate" DROP COLUMN IF EXISTS "watched_watch_name";
ALTER TABLE "agent_alert_candidate" RENAME COLUMN "watched_item_text" TO "watched_message_text";
ALTER TABLE "agent_alert_candidate" RENAME COLUMN "watched_item_at" TO "watched_message_at";
ALTER TABLE "agent_alert_candidate" RENAME COLUMN "watched_category" TO "watched_mail_category";
ALTER TABLE "agent_alert_candidate" RENAME COLUMN "watched_title" TO "watched_subject";
ALTER TABLE "agent_alert_candidate" RENAME COLUMN "watched_item_id" TO "watched_message_id";

DROP INDEX IF EXISTS "agent_watched_item_newest";
-- Versions of one item collapse to one row again.
DELETE FROM "agent_watched_item" WHERE ("agent_id", "skill_name", "watched_item_id", "watched_item_version") NOT IN (
    SELECT "agent_id", "skill_name", "watched_item_id", MIN("watched_item_version") FROM "agent_watched_item" GROUP BY "agent_id", "skill_name", "watched_item_id");
ALTER TABLE "agent_watched_item" DROP CONSTRAINT "agent_watched_item_pkey";
ALTER TABLE "agent_watched_item" DROP COLUMN "watched_item_version";
ALTER TABLE "agent_watched_item" DROP COLUMN "watch_name";
ALTER TABLE "agent_watched_item" RENAME COLUMN "watched_item_at" TO "watched_message_at";
ALTER TABLE "agent_watched_item" RENAME COLUMN "watched_item_id" TO "watched_message_id";
ALTER TABLE "agent_watched_item" RENAME TO "agent_watched_mail";
ALTER TABLE "agent_watched_mail" ADD CONSTRAINT "agent_watched_mail_pkey" PRIMARY KEY ("agent_id", "skill_name", "watched_message_id");
CREATE INDEX "agent_watched_mail_newest" ON "agent_watched_mail" ("agent_id", "skill_name", "watched_message_at" DESC);
