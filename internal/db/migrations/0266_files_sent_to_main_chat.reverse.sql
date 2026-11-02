DROP INDEX IF EXISTS "agent_attachment_main_chat_waiting";
ALTER TABLE "agent_attachment" DROP COLUMN IF EXISTS "main_chat_queued_at", DROP COLUMN IF EXISTS "sent_by", DROP COLUMN IF EXISTS "caption";
