DROP TABLE IF EXISTS "agent_alert_candidate";
ALTER TABLE "mail_insight" DROP COLUMN IF EXISTS "alert_reason";
ALTER TABLE "mail_insight" DROP COLUMN IF EXISTS "alert_signal";
