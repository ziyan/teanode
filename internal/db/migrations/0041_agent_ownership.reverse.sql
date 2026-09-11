DROP INDEX IF EXISTS "agent_job_open";
CREATE INDEX "agent_job_open" ON "agent_job" ("agent_id", "kind", "subject_id") WHERE "status" IN ('queued', 'running');
ALTER TABLE "agent_todo" DROP CONSTRAINT IF EXISTS "agent_todo_conversation_id_fkey";
ALTER TABLE "agent_mcp_connection" DROP CONSTRAINT IF EXISTS "agent_mcp_connection_agent_id_fkey";
ALTER TABLE "agent_schedule" DROP CONSTRAINT IF EXISTS "agent_schedule_agent_id_fkey";
ALTER TABLE "agent_memory" DROP CONSTRAINT IF EXISTS "agent_memory_agent_id_fkey";
ALTER TABLE "agent_feedback" DROP CONSTRAINT IF EXISTS "agent_feedback_agent_id_fkey";
ALTER TABLE "agent_reply" DROP CONSTRAINT IF EXISTS "agent_reply_agent_id_fkey";
