-- What the agent is keeping an eye on because mail said it is due, on its way
-- or coming up: a bill, a parcel, a form to return, an event. One row is one
-- situation, found by follow_up_key (the tracking number of a parcel, the
-- payee and due date of a bill), so the third message about a parcel updates
-- the row the first one made. Written by the extract run, closed by later
-- mail or a payment, and gone with the agent.
CREATE TABLE "agent_follow_up" (
    "id"               character varying(32)    NOT NULL PRIMARY KEY,
    "agent_id"         character varying(32)    NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,

    "follow_up_kind"   character varying(20)    NOT NULL,
    "follow_up_key"    text                     NOT NULL,
    "follow_up_title"  text                     NOT NULL DEFAULT '',

    -- Who a bill is to and who carries a parcel.
    "payee"            text                     NOT NULL DEFAULT '',
    "carrier"          text                     NOT NULL DEFAULT '',

    -- A bill's amount, in currency_code, not rounded.
    "amount"           numeric(19,4),
    "currency_code"    text                     NOT NULL DEFAULT '',
    "tracking_number"  text                     NOT NULL DEFAULT '',

    -- The day a bill or deadline is due, a parcel is expected, or an event
    -- happens, in the person's zone. NULL when the message gave none.
    "due_on"           date,
    "place"            text                     NOT NULL DEFAULT '',

    -- The page an event or deadline found on the web came from, so every
    -- surface can link to it. Empty for what mail said.
    "source_url"       text                     NOT NULL DEFAULT '',

    "follow_up_state"  character varying(20)    NOT NULL DEFAULT 'open',
    "closed_reason"    text                     NOT NULL DEFAULT '',

    -- The messages it came from, newest last.
    "source_item_ids"  jsonb                    NOT NULL DEFAULT '[]',

    -- What the person was last told about it (reminded, overdue, stalled,
    -- delivered or roundup), and when, so each is said once. Kept apart
    -- from follow_up_state: a bill is reminded while it is still open.
    "told_state"       character varying(20)    NOT NULL DEFAULT '',
    "told_at"          timestamp with time zone,

    "created_at"       timestamp with time zone NOT NULL,
    "updated_at"       timestamp with time zone NOT NULL,

    CHECK ("follow_up_kind" IN ('bill', 'parcel', 'deadline', 'event')),
    CHECK ("follow_up_state" IN ('open', 'closed', 'overdue', 'stalled', 'expired', 'stopped')),
    CHECK ("told_state" IN ('', 'reminded', 'overdue', 'stalled', 'delivered', 'roundup'))
);
CREATE UNIQUE INDEX "agent_follow_up_key" ON "agent_follow_up" ("agent_id", "follow_up_key");
CREATE INDEX "agent_follow_up_open" ON "agent_follow_up" ("agent_id", "due_on")
    WHERE "follow_up_state" IN ('open', 'overdue', 'stalled');

-- A follow-up that is due to be told is an alert candidate of kind
-- follow_up (candidate_kind has no check, so the kind needs no widening): the
-- follow-ups it is about (one, or several for the Sunday roundup), what is
-- being told about them, and the key of the alert that tells it, the
-- follow-up's key and the told state, or follow-up-roundup:<the Sunday's
-- date>. Unique, so two sweeps running together cannot both write it.
ALTER TABLE "agent_alert_candidate"
    ADD COLUMN "follow_up_ids"         jsonb                 NOT NULL DEFAULT '[]',
    ADD COLUMN "follow_up_told_state"  character varying(20) NOT NULL DEFAULT '',
    ADD COLUMN "follow_up_subject_key" text                  NOT NULL DEFAULT '';
CREATE UNIQUE INDEX "agent_alert_candidate_follow_up" ON "agent_alert_candidate" ("agent_id", "follow_up_subject_key") WHERE "follow_up_subject_key" <> '';

-- When the one look back over the last 45 days of mail ran for this agent, so
-- that bills and parcels already in flight are on the list the day it ships
-- and the look never runs twice. NULL until it has. Claimed with a single
-- conditional update, so two workers cannot both run it.
ALTER TABLE "agent" ADD COLUMN "follow_up_backfilled_at" timestamp with time zone;
