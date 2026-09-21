-- Which build of this program last wrote a page and a fact.
--
-- A graph outlives the code that filled it. The first real ingest filed
-- twenty-eight facts of the shape "X is a project or work channel" --
-- true, worthless, and indistinguishable from knowledge at a glance --
-- because the prompt of the day invited them. The prompt is fixed and
-- the rule is now in code, but nothing could find what the old build had
-- already written except a person with a regular expression.
--
-- With the version on the row, a newer build can go back over what an
-- older one wrote and apply what it knows now. That is the whole reason
-- to keep a version here rather than a timestamp: "written before the
-- rule existed" is the question, and a release is how that is named.
--
-- Empty means "before this column", which is exactly the rows most
-- likely to need looking at.
ALTER TABLE "agent_node" ADD COLUMN "version" character varying(40) NOT NULL DEFAULT '';
ALTER TABLE "agent_fact" ADD COLUMN "version" character varying(40) NOT NULL DEFAULT '';
ALTER TABLE "agent_revision" ADD COLUMN "version" character varying(40) NOT NULL DEFAULT '';

-- Finding what an old build wrote is a scan of one agent's facts, so the
-- version leads and the agent follows.
CREATE INDEX "agent_fact_version" ON "agent_fact" ("agent_id", "version");

-- And what a night struck because an older build had written it.
ALTER TABLE "agent_dream" ADD COLUMN "revised" integer NOT NULL DEFAULT 0;
