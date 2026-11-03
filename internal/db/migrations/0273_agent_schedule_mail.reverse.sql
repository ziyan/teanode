-- Schedules that ran on arriving mail have no time to run at once the
-- conditions are gone, so they go with them.
DELETE FROM "agent_schedule" WHERE "mail_conditions" IS NOT NULL;
ALTER TABLE "agent_schedule" DROP COLUMN "mail_arrivals";
ALTER TABLE "agent_schedule" DROP COLUMN "mail_conditions";
