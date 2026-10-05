-- A goal is background work with a conversation of its own, of the kind
-- goal, where its turns run out of the person's sight. Its description is
-- the existing goal text and its status line the existing goal note; what
-- it is called in a list is new.
ALTER TABLE "agent_conversation" ADD COLUMN "goal_title" text NOT NULL DEFAULT '';

-- When a goal that came to need the person was said in the main
-- conversation. Empty while it waits and has not been: the worker's sweep
-- says it there, once, as soon as no turn of the person's is running, and
-- writes the time so a restart does not say it twice.
ALTER TABLE "agent_conversation" ADD COLUMN "goal_surfaced_at" timestamp with time zone;
CREATE INDEX "agent_conversation_goal_to_surface"
    ON "agent_conversation" ("agent_id")
    WHERE "kind" = 'goal' AND "goal_state" = 'waiting' AND "goal_surfaced_at" IS NULL;

-- What happened on a goal, for the person to read: one row when it was
-- started, when it came to need them, when they answered, when it was met
-- or dropped, and when a turn of its own says something happened. A turn
-- that only looked and found nothing writes none. Gone with the goal.
CREATE TABLE "agent_goal_activity" (
    "id"                 character varying(32)    NOT NULL PRIMARY KEY,
    "agent_id"           character varying(32)    NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,
    "conversation_id"    character varying(32)    NOT NULL REFERENCES "agent_conversation" ("id") ON DELETE CASCADE,
    "created_at"         timestamp with time zone NOT NULL,
    "goal_activity_kind" text                     NOT NULL,
    "activity_headline"  text                     NOT NULL DEFAULT '',
    "activity_detail"    text                     NOT NULL DEFAULT ''
);
CREATE INDEX "agent_goal_activity_conversation" ON "agent_goal_activity" ("conversation_id", "created_at" DESC);

-- What a goal made that carries no conversation of its own: a mail rule,
-- a reminder, an alert mute. Schedules and background work already say
-- which conversation made them, and are found that way.
CREATE TABLE "agent_goal_artifact" (
    "id"                 character varying(32)    NOT NULL PRIMARY KEY,
    "agent_id"           character varying(32)    NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,
    "conversation_id"    character varying(32)    NOT NULL REFERENCES "agent_conversation" ("id") ON DELETE CASCADE,
    "created_at"         timestamp with time zone NOT NULL,
    "goal_artifact_kind" text                     NOT NULL,
    "artifact_reference" text                     NOT NULL DEFAULT '',
    "artifact_title"     text                     NOT NULL DEFAULT ''
);
CREATE INDEX "agent_goal_artifact_conversation" ON "agent_goal_artifact" ("conversation_id", "created_at");
