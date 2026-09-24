-- Runs by the job that made them. A dream's runs are listed by its job,
-- and what a dream cost is the sum of what its runs cost, read for every
-- dream a page shows.
CREATE INDEX "agent_conversation_job" ON "agent_conversation" ("job_id") WHERE "job_id" <> '';
