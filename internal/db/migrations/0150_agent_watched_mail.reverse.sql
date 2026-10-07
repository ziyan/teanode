ALTER TABLE "agent_alert_candidate"
    DROP COLUMN IF EXISTS "watched_skill_name",
    DROP COLUMN IF EXISTS "watched_message_id",
    DROP COLUMN IF EXISTS "watched_sender",
    DROP COLUMN IF EXISTS "watched_subject",
    DROP COLUMN IF EXISTS "watched_mail_category",
    DROP COLUMN IF EXISTS "watched_message_at",
    DROP COLUMN IF EXISTS "watched_message_text";
DROP TABLE IF EXISTS "agent_watched_mail";
