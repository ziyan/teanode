-- Whether a message is worth telling the person about now, as the sorting
-- judged it: none, soon or now, and a line saying why.
ALTER TABLE "mail_insight" ADD COLUMN "alert_signal" character varying(10) NOT NULL DEFAULT 'none';
ALTER TABLE "mail_insight" ADD COLUMN "alert_reason" text NOT NULL DEFAULT '';

-- What might be worth telling the person without being asked: a message
-- the sorting said so of, or a burst of messages alike that a count
-- noticed. Candidates gather here until the alert job reads them, tells
-- the person about some and drops the rest, saying why. Gone with the
-- agent.
CREATE TABLE "agent_alert_candidate" (
    "id"               character varying(32)    NOT NULL PRIMARY KEY,
    "agent_id"         character varying(32)    NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,
    "mailbox_id"       character varying(32)    NOT NULL DEFAULT '',

    -- The message it is about: the one the sorting judged, or the latest
    -- of a burst.
    "mail_id"          character varying(32)    NOT NULL DEFAULT '',

    -- message or burst.
    "candidate_kind"   character varying(20)    NOT NULL,

    -- The sorting's signal and its line, or what the count saw.
    "alert_signal"     character varying(10)    NOT NULL DEFAULT 'none',
    "candidate_reason" text                     NOT NULL DEFAULT '',

    -- A burst's sender domain and subject with the digits taken out, and
    -- how many messages it counted.
    "burst_key"        text                     NOT NULL DEFAULT '',
    "burst_count"      integer                  NOT NULL DEFAULT 0,

    "created_at"       timestamp with time zone NOT NULL,

    -- The alert that told the person about it, or when and why it was
    -- dropped; both empty while it waits.
    "alert_id"         character varying(32)    NOT NULL DEFAULT '',
    "dropped_at"       timestamp with time zone,
    "drop_reason"      text                     NOT NULL DEFAULT ''
);
CREATE INDEX "agent_alert_candidate_waiting" ON "agent_alert_candidate" ("agent_id", "created_at") WHERE "alert_id" = '' AND "dropped_at" IS NULL;
CREATE INDEX "agent_alert_candidate_burst" ON "agent_alert_candidate" ("agent_id", "mailbox_id", "burst_key", "created_at" DESC) WHERE "burst_key" <> '';
