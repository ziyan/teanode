-- A watch now belongs to a skill and says what it watches (mail, a
-- transaction, a mention), so what was a watched message is a watched
-- item: kept by the skill and the watch that found it, and by a version,
-- so that an item that changed (a thread with a new reply) is looked at
-- again.
ALTER TABLE "agent_watched_mail" RENAME TO "agent_watched_item";
ALTER TABLE "agent_watched_item" RENAME COLUMN "watched_message_id" TO "watched_item_id";
ALTER TABLE "agent_watched_item" RENAME COLUMN "watched_message_at" TO "watched_item_at";
ALTER TABLE "agent_watched_item" ADD COLUMN "watch_name" character varying(100) NOT NULL DEFAULT '';
ALTER TABLE "agent_watched_item" ADD COLUMN "watched_item_version" character varying(200) NOT NULL DEFAULT '';
-- Forgotten: the Gmail watch's records were by message, and a watch now
-- records a thread by its message count. The next look of each watch is
-- a first look, which only takes note of what is there.
DELETE FROM "agent_watched_item";
ALTER TABLE "agent_watched_item" DROP CONSTRAINT "agent_watched_mail_pkey";
ALTER TABLE "agent_watched_item" ADD PRIMARY KEY ("agent_id", "skill_name", "watch_name", "watched_item_id", "watched_item_version");
DROP INDEX IF EXISTS "agent_watched_mail_newest";
CREATE INDEX "agent_watched_item_newest" ON "agent_watched_item" ("agent_id", "skill_name", "watch_name", "watched_item_at" DESC);

-- The candidate's columns, for an item of any kind.
ALTER TABLE "agent_alert_candidate" RENAME COLUMN "watched_message_id" TO "watched_item_id";
ALTER TABLE "agent_alert_candidate" RENAME COLUMN "watched_subject" TO "watched_title";
ALTER TABLE "agent_alert_candidate" RENAME COLUMN "watched_mail_category" TO "watched_category";
ALTER TABLE "agent_alert_candidate" RENAME COLUMN "watched_message_at" TO "watched_item_at";
ALTER TABLE "agent_alert_candidate" RENAME COLUMN "watched_message_text" TO "watched_item_text";
ALTER TABLE "agent_alert_candidate" ADD COLUMN "watched_watch_name" character varying(100) NOT NULL DEFAULT '';
ALTER TABLE "agent_alert_candidate" ADD COLUMN "watched_item_url" text NOT NULL DEFAULT '';
UPDATE "agent_alert_candidate" SET "watched_watch_name" = 'new_mail' WHERE "watched_skill_name" = 'gmail';
