-- What belongs to an agent goes with the agent, and what belongs to a
-- conversation with the conversation: forgetting an agent or deleting a
-- person used to leave replies, corrections, memories, schedules, tasks
-- and sealed connections behind. Rows that already lost their agent are
-- removed first, or the constraints could not be added.
DELETE FROM "agent_reply" WHERE "agent_id" NOT IN (SELECT "id" FROM "agent");
DELETE FROM "agent_feedback" WHERE "agent_id" NOT IN (SELECT "id" FROM "agent");
DELETE FROM "agent_memory" WHERE "agent_id" NOT IN (SELECT "id" FROM "agent");
DELETE FROM "agent_schedule" WHERE "agent_id" NOT IN (SELECT "id" FROM "agent");
DELETE FROM "agent_mcp_connection" WHERE "agent_id" NOT IN (SELECT "id" FROM "agent");
DELETE FROM "agent_todo" WHERE "conversation_id" NOT IN (SELECT "id" FROM "agent_conversation");
ALTER TABLE "agent_reply" ADD CONSTRAINT "agent_reply_agent_id_fkey" FOREIGN KEY ("agent_id") REFERENCES "agent" ("id") ON DELETE CASCADE;
ALTER TABLE "agent_feedback" ADD CONSTRAINT "agent_feedback_agent_id_fkey" FOREIGN KEY ("agent_id") REFERENCES "agent" ("id") ON DELETE CASCADE;
ALTER TABLE "agent_memory" ADD CONSTRAINT "agent_memory_agent_id_fkey" FOREIGN KEY ("agent_id") REFERENCES "agent" ("id") ON DELETE CASCADE;
ALTER TABLE "agent_schedule" ADD CONSTRAINT "agent_schedule_agent_id_fkey" FOREIGN KEY ("agent_id") REFERENCES "agent" ("id") ON DELETE CASCADE;
ALTER TABLE "agent_mcp_connection" ADD CONSTRAINT "agent_mcp_connection_agent_id_fkey" FOREIGN KEY ("agent_id") REFERENCES "agent" ("id") ON DELETE CASCADE;
ALTER TABLE "agent_todo" ADD CONSTRAINT "agent_todo_conversation_id_fkey" FOREIGN KEY ("conversation_id") REFERENCES "agent_conversation" ("id") ON DELETE CASCADE;

-- One open job per agent, kind and subject: the index that found the
-- duplicate is unique now, so two deliveries queuing at once insert one.
DROP INDEX IF EXISTS "agent_job_open";
CREATE UNIQUE INDEX "agent_job_open" ON "agent_job" ("agent_id", "kind", "subject_id") WHERE "status" IN ('queued', 'running');
