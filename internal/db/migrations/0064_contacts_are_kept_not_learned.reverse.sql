-- The table comes back empty: what it held was learned from mail that has
-- already been delivered, and nothing re-learns it.
DROP TABLE IF EXISTS "mailbox_auto_reply";

CREATE TABLE "mailbox_contact" (
    "mailbox_id"      character varying(32)    NOT NULL REFERENCES "mailbox" ("id") ON DELETE CASCADE,
    "address"         character varying(320)   NOT NULL,
    "name"            character varying(256)   NOT NULL DEFAULT '',
    "last_seen_at"    timestamp with time zone NOT NULL,
    "count"           integer                  NOT NULL DEFAULT 1,
    "auto_replied_at" timestamp with time zone,
    PRIMARY KEY ("mailbox_id", "address")
);
