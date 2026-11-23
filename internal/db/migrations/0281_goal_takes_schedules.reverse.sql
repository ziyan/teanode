CREATE TABLE "agent_schedule" (
    "id"                      character varying(32)    NOT NULL PRIMARY KEY,
    "created_at"              timestamp with time zone NOT NULL,
    "modified_at"             timestamp with time zone NOT NULL,
    "agent_id"                character varying(32)    NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,
    "name"                    character varying(200)   NOT NULL,
    "cron"                    character varying(100)   NOT NULL,
    "prompt"                  text                     NOT NULL,
    "deliver"                 character varying(16)    NOT NULL DEFAULT 'drawer',
    "enabled"                 boolean                  NOT NULL DEFAULT true,
    "last_run_at"             timestamp with time zone,
    "next_run_at"             timestamp with time zone,
    "written_by"              character varying(16)    NOT NULL DEFAULT 'person',
    "conversation_id"         character varying(32)    NOT NULL DEFAULT '',
    "mail_conditions"         jsonb,
    "mail_arrivals"           jsonb,
    "is_notified_when_posted" boolean                  NOT NULL DEFAULT false
);
CREATE INDEX "agent_schedule_due" ON "agent_schedule" ("enabled", "next_run_at");

-- Each time a goal runs at is a schedule of that goal's again, and the mail
-- it waits for one more. The schedules answer in the goal's conversation,
-- as the ones a goal owned did.
INSERT INTO "agent_schedule" (
    "id", "created_at", "modified_at", "agent_id", "name", "cron", "prompt", "deliver", "enabled",
    "next_run_at", "written_by", "conversation_id", "is_notified_when_posted")
SELECT
    CASE WHEN "time"."position" = 1 THEN "goal"."id" ELSE left("goal"."id", 26) || '-' || "time"."position" END,
    COALESCE("goal"."goal_set_at", "goal"."created_at"), now(), "goal"."agent_id", left("goal"."goal_title", 200),
    left("time"."cron", 100), "goal"."goal", 'drawer', "goal"."goal_state" = 'working',
    CASE WHEN "goal"."goal_state" = 'working' THEN "goal"."goal_next_at" END,
    CASE WHEN "goal"."goal_written_by" IN ('person', 'agent', 'built_in') THEN "goal"."goal_written_by" ELSE 'agent' END,
    "goal"."id", "goal"."is_goal_notified_when_posted"
FROM "agent_conversation" AS "goal",
    jsonb_array_elements_text(COALESCE("goal"."goal_times", '[]')) WITH ORDINALITY AS "time" ("cron", "position")
WHERE "goal"."kind" = 'goal';

INSERT INTO "agent_schedule" (
    "id", "created_at", "modified_at", "agent_id", "name", "cron", "prompt", "deliver", "enabled",
    "next_run_at", "written_by", "conversation_id", "mail_conditions", "mail_arrivals", "is_notified_when_posted")
SELECT
    left("goal"."id", 26) || '-mail', COALESCE("goal"."goal_set_at", "goal"."created_at"), now(), "goal"."agent_id",
    left("goal"."goal_title", 200), '', "goal"."goal", 'drawer', "goal"."goal_state" = 'working',
    CASE WHEN "goal"."goal_state" = 'working' AND "goal"."goal_mail_arrivals" IS NOT NULL THEN now() END,
    CASE WHEN "goal"."goal_written_by" IN ('person', 'agent', 'built_in') THEN "goal"."goal_written_by" ELSE 'agent' END,
    "goal"."id", "goal"."goal_mail_conditions", "goal"."goal_mail_arrivals", "goal"."is_goal_notified_when_posted"
FROM "agent_conversation" AS "goal"
WHERE "goal"."kind" = 'goal' AND "goal"."goal_mail_conditions" IS NOT NULL;

-- The older release knows no paused goal: one waiting for its person is
-- the nearest, and its schedules are off.
UPDATE "agent_conversation" SET "goal_state" = 'waiting' WHERE "kind" = 'goal' AND "goal_state" = 'paused';

UPDATE "agent_feed_item" SET "feed_source_kind" = 'schedule'
WHERE "feed_source_kind" = 'goal' AND "feed_source_id" IN (SELECT "id" FROM "agent_schedule");

ALTER TABLE "agent_conversation"
    DROP COLUMN "goal_times",
    DROP COLUMN "goal_mail_conditions",
    DROP COLUMN "goal_mail_arrivals",
    DROP COLUMN "goal_written_by",
    DROP COLUMN "is_goal_notified_when_posted";
