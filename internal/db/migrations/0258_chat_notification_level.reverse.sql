ALTER TABLE "chat_message" DROP COLUMN IF EXISTS "is_all_mentioned";
ALTER TABLE "chat_message" DROP COLUMN IF EXISTS "mentioned_user_ids";

-- A member who turned a chat off is muted with no end in sight; one told only
-- of mentions is told of everything again, the nearest the mute comes to it.
ALTER TABLE "chat_member" ADD COLUMN IF NOT EXISTS "muted_until" timestamp with time zone;
UPDATE "chat_member" SET "muted_until" = '2999-12-31T00:00:00Z' WHERE "notification_level" = 'off';
ALTER TABLE "chat_member" DROP COLUMN IF EXISTS "notification_level";
