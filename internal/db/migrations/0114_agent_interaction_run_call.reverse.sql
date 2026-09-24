DROP INDEX IF EXISTS "agent_interaction_run_call";
CREATE INDEX "agent_interaction_call" ON "agent_interaction" ("agent_id", "call_id");
