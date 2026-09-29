-- An overview is how the thing a page is about works: several short
-- sections written by the night from the page's facts, the overviews of the
-- pages under it and the openings of the pages it is linked to. It is not
-- the opening, which says what the page is in one paragraph and is carried
-- by every prompt's index.
--
-- overview_inputs is a hash of what the overview was written from. The night
-- rewrites an overview only when the hash of the page's inputs as they are
-- now differs from it, so an empty one is due, and clearing it is how a
-- person asks for a page to be written again.
--
-- overview_evidence is the pages and files the overview cites, as the same
-- evidence objects a fact carries.
ALTER TABLE "agent_node" ADD COLUMN "overview" text NOT NULL DEFAULT '';
ALTER TABLE "agent_node" ADD COLUMN "overview_written_at" timestamp with time zone;
ALTER TABLE "agent_node" ADD COLUMN "overview_inputs" character varying(64) NOT NULL DEFAULT '';
ALTER TABLE "agent_node" ADD COLUMN "overview_evidence" jsonb NOT NULL DEFAULT '[]';

-- How many overviews a night wrote.
ALTER TABLE "agent_dream" ADD COLUMN "overviews_written" integer NOT NULL DEFAULT 0;
