DROP TABLE IF EXISTS "agent_alert_mute";
ALTER TABLE "agent" DROP COLUMN IF EXISTS "alert_daily_most";
ALTER TABLE "agent" DROP COLUMN IF EXISTS "alert_quiet_end";
ALTER TABLE "agent" DROP COLUMN IF EXISTS "alert_quiet_start";
ALTER TABLE "agent" DROP COLUMN IF EXISTS "is_alerts_enabled";
