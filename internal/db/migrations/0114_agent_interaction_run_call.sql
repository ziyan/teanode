-- A card is named by the run that raised it and the call: some providers
-- number calls afresh in every answer, so a call id alone names many.
DROP INDEX IF EXISTS "agent_interaction_call";
CREATE UNIQUE INDEX "agent_interaction_run_call" ON "agent_interaction" ("run_id", "call_id");
