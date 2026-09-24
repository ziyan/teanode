-- When the agent last spoke first, when onboarding ended, whether it may
-- start a memory check or give a tip, and until when the person asked it
-- to keep quiet. The agent speaks first in the main conversation for three
-- reasons -- onboarding, a memory check, a tip -- and these are what its
-- sweep reads to decide whether one is welcome.
ALTER TABLE "agent" ADD COLUMN "spoke_first_at" timestamp with time zone;
ALTER TABLE "agent" ADD COLUMN "onboarded_at" timestamp with time zone;
ALTER TABLE "agent" ADD COLUMN "is_memory_check_enabled" boolean NOT NULL DEFAULT true;
ALTER TABLE "agent" ADD COLUMN "is_tips_enabled" boolean NOT NULL DEFAULT true;
ALTER TABLE "agent" ADD COLUMN "speak_first_snoozed_until" timestamp with time zone;
-- An agent already in use has been introduced; only new ones are onboarded.
UPDATE "agent" SET "onboarded_at" = now();
