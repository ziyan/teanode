-- Stop workers before downgrade; unfinished work is claimed again by the older binary.
UPDATE "agent_job" SET "status" = 'queued', "claimed_at" = NULL, "claimed_by" = '', "finished_at" = NULL WHERE "status" = 'running';
ALTER TABLE "agent_job" DROP COLUMN "claim_id";
ALTER TABLE "agent_job" DROP COLUMN "failure_count";
