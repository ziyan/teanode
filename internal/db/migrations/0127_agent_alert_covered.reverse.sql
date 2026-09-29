ALTER TABLE "agent_alert" DROP COLUMN IF EXISTS "covered_mail_categories";
ALTER TABLE "agent_alert" DROP COLUMN IF EXISTS "covered_sender_domains";
ALTER TABLE "agent_alert" DROP COLUMN IF EXISTS "covered_sender_addresses";
ALTER TABLE "agent_alert" DROP COLUMN IF EXISTS "covered_burst_keys";
