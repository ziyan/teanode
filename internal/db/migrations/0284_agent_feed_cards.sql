-- A card whose problem cleared is marked resolved instead of being followed by
-- an "all clear" card: when, the one line saying what cleared it, and who
-- resolved it (a goal, the person or the agent, with the goal's conversation id
-- or the person's user id). A card can also carry charts: a series over time or
-- bars, each with the line saying where and when its numbers were read. Every
-- column has a default, so the previous binary reads and writes the table as
-- before.
ALTER TABLE "agent_feed_item" ADD COLUMN "resolved_at" timestamptz NULL;
ALTER TABLE "agent_feed_item" ADD COLUMN "resolution_note" text NOT NULL DEFAULT '';
ALTER TABLE "agent_feed_item" ADD COLUMN "resolved_by_kind" text NOT NULL DEFAULT '';
ALTER TABLE "agent_feed_item" ADD COLUMN "resolved_by_id" text NOT NULL DEFAULT '';
ALTER TABLE "agent_feed_item" ADD COLUMN "chart_blocks" jsonb NOT NULL DEFAULT '[]';
