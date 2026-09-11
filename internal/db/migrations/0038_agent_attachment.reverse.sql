ALTER TABLE "agent_message" DROP COLUMN IF EXISTS "references";
ALTER TABLE "agent_message" DROP COLUMN IF EXISTS "attachments";
DROP TABLE IF EXISTS "agent_attachment";
