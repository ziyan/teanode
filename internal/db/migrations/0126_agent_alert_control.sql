-- The person's say over alerts. On for every agent, the ones there already
-- included, since the point is that nobody had to ask; the night and the
-- day's most are empty and zero for the defaults, 22:00 to 07:00 and five,
-- as the dream's hours are.
ALTER TABLE "agent" ADD COLUMN "is_alerts_enabled" boolean NOT NULL DEFAULT true;
ALTER TABLE "agent" ADD COLUMN "alert_quiet_start" character varying(5) NOT NULL DEFAULT '';
ALTER TABLE "agent" ADD COLUMN "alert_quiet_end" character varying(5) NOT NULL DEFAULT '';
ALTER TABLE "agent" ADD COLUMN "alert_daily_most" integer NOT NULL DEFAULT 0;

-- "Don't tell me about these": a sender, a domain, a subject key or a kind
-- of alert the agent says nothing about. Read by the candidate step and
-- the alert job, which drop what matches as muted. Gone with the agent.
CREATE TABLE "agent_alert_mute" (
    "id"          character varying(32)    NOT NULL PRIMARY KEY,
    "agent_id"    character varying(32)    NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,

    -- sender, domain, subjectKey or kind, and the address, domain, key or
    -- kind it names, in lower case.
    "mute_scope"  character varying(20)    NOT NULL,
    "mute_target" text                     NOT NULL,

    -- The alert it was muted from, if it was.
    "alert_id"    character varying(32)    NOT NULL DEFAULT '',

    "created_at"  timestamp with time zone NOT NULL
);
CREATE UNIQUE INDEX "agent_alert_mute_target" ON "agent_alert_mute" ("agent_id", "mute_scope", "mute_target");
