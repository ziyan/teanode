-- Historical attempts include deferrals, so they cannot initialize a failure count.
-- Drain workers from older binaries before applying this migration.
ALTER TABLE "agent_job" ADD COLUMN "failure_count" integer NOT NULL DEFAULT 0 CHECK ("failure_count" >= 0);
ALTER TABLE "agent_job" ADD COLUMN "claim_id" character varying(32) NOT NULL DEFAULT '';
UPDATE "agent_job" SET "status" = 'queued', "claimed_at" = NULL, "claimed_by" = '', "finished_at" = NULL WHERE "status" = 'running';
