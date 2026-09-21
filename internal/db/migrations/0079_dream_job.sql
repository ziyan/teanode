-- A dream remembers the job that ran it. Every model call a dream makes
-- is a run of the conversation loop, a conversation of kind run tagged
-- with that job, so the dream log can list a dream's runs and the person
-- can open each one, as they can a sorting run.
ALTER TABLE "agent_dream" ADD COLUMN "job_id" character varying(32) NOT NULL DEFAULT '';
