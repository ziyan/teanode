-- When the night last reflected on a page: the overview's own time as it
-- was read for the reflection, so a theme is due again when its overview
-- is written after it. Kept on the page rather than worked out from the
-- reflections themselves, because a reflection that finds nothing worth
-- saying writes no fact, and a theme with nothing to say would otherwise
-- be asked again every night.
ALTER TABLE "agent_node" ADD COLUMN "reflected_at" timestamp with time zone;

-- What a night did with its themes and reflections.
ALTER TABLE "agent_dream" ADD COLUMN "themes_made" integer NOT NULL DEFAULT 0;
ALTER TABLE "agent_dream" ADD COLUMN "themes_updated" integer NOT NULL DEFAULT 0;
ALTER TABLE "agent_dream" ADD COLUMN "reflections_written" integer NOT NULL DEFAULT 0;
