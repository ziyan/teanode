-- A schedule becomes a goal. A goal keeps the times it runs at (cron lines
-- in the person's zone, or "@at" and a moment), the mail it waits for and
-- the messages that came, who wrote its instruction, and whether its first
-- card of a run notifies. A goal with none of these keeps its own cadence.
ALTER TABLE "agent_conversation"
    ADD COLUMN "goal_times"                   jsonb,
    ADD COLUMN "goal_mail_conditions"         jsonb,
    ADD COLUMN "goal_mail_arrivals"           jsonb,
    ADD COLUMN "goal_written_by"              text    NOT NULL DEFAULT '',
    ADD COLUMN "is_goal_notified_when_posted" boolean NOT NULL DEFAULT false;

-- The schedules a goal owned, the ones made in its conversation, are its
-- times now. What each said to do is kept in the goal's description under
-- its name and time, and the goal is due when the soonest of them was. One
-- that was switched off gives the goal no time, but what it said is kept,
-- marked off, so that nothing it held is lost.
WITH "owned_all" AS (
    SELECT "schedule".*
    FROM "agent_schedule" AS "schedule"
    JOIN "agent_conversation" AS "goal"
        ON "goal"."id" = "schedule"."conversation_id" AND "goal"."agent_id" = "schedule"."agent_id" AND "goal"."kind" = 'goal'
), "owned" AS (
    SELECT * FROM "owned_all" WHERE "owned_all"."enabled"
)
UPDATE "agent_conversation" AS "goal" SET
    "goal_times" = (
        SELECT jsonb_agg("owned"."cron" ORDER BY "owned"."created_at")
        FROM "owned" WHERE "owned"."conversation_id" = "goal"."id" AND "owned"."cron" <> ''),
    "goal_mail_conditions" = (
        SELECT "owned"."mail_conditions" FROM "owned"
        WHERE "owned"."conversation_id" = "goal"."id" AND "owned"."mail_conditions" IS NOT NULL
        ORDER BY "owned"."created_at" LIMIT 1),
    "goal_mail_arrivals" = (
        SELECT "owned"."mail_arrivals" FROM "owned"
        WHERE "owned"."conversation_id" = "goal"."id" AND "owned"."mail_conditions" IS NOT NULL
        ORDER BY "owned"."created_at" LIMIT 1),
    "is_goal_notified_when_posted" = COALESCE((
        SELECT bool_or("owned"."is_notified_when_posted") FROM "owned" WHERE "owned"."conversation_id" = "goal"."id"),
        "goal"."is_goal_notified_when_posted"),
    "goal_written_by" = 'agent',
    "goal" = "goal"."goal" || E'\n\n' || (
        SELECT string_agg(
            "owned_all"."name" || ' (' || CASE WHEN "owned_all"."cron" = '' THEN 'when the mail it waits for arrives' ELSE "owned_all"."cron" END
                || CASE WHEN "owned_all"."enabled" THEN '' ELSE ', switched off' END || '): ' || "owned_all"."prompt",
            E'\n\n' ORDER BY "owned_all"."created_at")
        FROM "owned_all" WHERE "owned_all"."conversation_id" = "goal"."id"),
    "goal_next_at" = CASE WHEN "goal"."goal_state" = 'working' THEN LEAST("goal"."goal_next_at", (
        SELECT min("owned"."next_run_at") FROM "owned" WHERE "owned"."conversation_id" = "goal"."id")) ELSE "goal"."goal_next_at" END
WHERE EXISTS (SELECT 1 FROM "owned_all" WHERE "owned_all"."conversation_id" = "goal"."id");

-- Every other schedule is a goal of its own, with the schedule's id, so
-- that the cards it posted name it. A schedule that was off is a paused
-- goal.
INSERT INTO "agent_conversation" (
    "id", "created_at", "modified_at", "agent_id", "kind", "title", "titled_by", "surface", "last_at",
    "goal", "goal_title", "goal_state", "goal_next_at", "goal_set_at", "goal_origin_conversation_id",
    "goal_times", "goal_mail_conditions", "goal_mail_arrivals", "goal_written_by", "is_goal_notified_when_posted")
SELECT
    "schedule"."id", "schedule"."created_at", now(), "schedule"."agent_id", 'goal', "schedule"."name", 'program', 'goal',
    COALESCE("schedule"."last_run_at", "schedule"."created_at"),
    "schedule"."prompt", "schedule"."name",
    CASE WHEN "schedule"."enabled" THEN 'working' ELSE 'paused' END,
    CASE WHEN "schedule"."enabled" THEN "schedule"."next_run_at" END,
    "schedule"."created_at",
    (SELECT "origin"."id" FROM "agent_conversation" AS "origin"
        WHERE "origin"."id" = "schedule"."conversation_id" AND "origin"."agent_id" = "schedule"."agent_id"
            AND "origin"."kind" IN ('main', 'named')),
    CASE WHEN "schedule"."cron" <> '' THEN jsonb_build_array("schedule"."cron") END,
    "schedule"."mail_conditions", "schedule"."mail_arrivals",
    -- The brief and the asset estimates, made by their switches before a
    -- switch's schedule was recorded as TeaNode's, read content the person
    -- did not write, so they are not taken for the person's own.
    CASE WHEN lower("schedule"."name") IN ('daily brief', 'estimate asset values') AND "schedule"."written_by" = 'person'
        THEN 'built_in' ELSE "schedule"."written_by" END,
    "schedule"."is_notified_when_posted"
FROM "agent_schedule" AS "schedule"
WHERE NOT EXISTS (
    SELECT 1 FROM "agent_conversation" AS "goal"
    WHERE "goal"."id" = "schedule"."conversation_id" AND "goal"."agent_id" = "schedule"."agent_id" AND "goal"."kind" = 'goal');

-- The cards a schedule posted are its goal's.
UPDATE "agent_feed_item" AS "item" SET
    "feed_source_kind" = 'goal',
    "feed_source_id" = COALESCE((
        SELECT "goal"."id" FROM "agent_schedule" AS "schedule"
        JOIN "agent_conversation" AS "goal"
            ON "goal"."id" = "schedule"."conversation_id" AND "goal"."agent_id" = "schedule"."agent_id" AND "goal"."kind" = 'goal'
        WHERE "schedule"."id" = "item"."feed_source_id"), "item"."feed_source_id")
WHERE "item"."feed_source_kind" = 'schedule';

-- A schedule's run still waiting in the queue has nothing to run it.
DELETE FROM "agent_job" WHERE "kind" = 'schedule' AND "status" IN ('queued', 'running');

DROP TABLE "agent_schedule";
