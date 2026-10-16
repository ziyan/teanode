ALTER TABLE "agent" DROP COLUMN IF EXISTS "follow_up_backfilled_at";
DROP INDEX IF EXISTS "agent_alert_candidate_follow_up";
ALTER TABLE "agent_alert_candidate" DROP COLUMN IF EXISTS "follow_up_subject_key";
ALTER TABLE "agent_alert_candidate" DROP COLUMN IF EXISTS "follow_up_told_state";
ALTER TABLE "agent_alert_candidate" DROP COLUMN IF EXISTS "follow_up_ids";
DROP TABLE IF EXISTS "agent_follow_up";
