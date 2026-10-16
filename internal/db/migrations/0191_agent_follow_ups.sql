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

    "follow_up_state"  character varying(20)    NOT NULL DEFAULT 'open',
    "closed_reason"    text                     NOT NULL DEFAULT '',

    -- The messages it came from, newest last.
    "source_item_ids"  jsonb                    NOT NULL DEFAULT '[]',

    -- The state the person was last told about, and when, so a state is
    -- said once.
    "told_state"       character varying(20)    NOT NULL DEFAULT '',
    "told_at"          timestamp with time zone,

    "created_at"       timestamp with time zone NOT NULL,
    "updated_at"       timestamp with time zone NOT NULL,

    CHECK ("follow_up_kind" IN ('bill', 'parcel', 'deadline', 'event')),
    CHECK ("follow_up_state" IN ('open', 'closed', 'overdue', 'stalled', 'expired', 'stopped'))
);
CREATE UNIQUE INDEX "agent_follow_up_key" ON "agent_follow_up" ("agent_id", "follow_up_key");
CREATE INDEX "agent_follow_up_open" ON "agent_follow_up" ("agent_id", "due_on")
    WHERE "follow_up_state" IN ('open', 'overdue', 'stalled');
