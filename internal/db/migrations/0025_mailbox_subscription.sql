-- What a person asked for, which the mail itself cannot say.
--
-- A subscription is a grouping of messages that named the same list, and it
-- exists for as long as that mail does. The request to leave it outlives the
-- mail: somebody who unsubscribes and then deletes every message should still
-- see that they asked, and asking twice should not be the way to find out
-- whether the first one worked.
CREATE TABLE IF NOT EXISTS "mailbox_subscription" (
    "id"           character varying(32)    NOT NULL,
    "created_at"   timestamp with time zone NOT NULL,
    "modified_at"  timestamp with time zone NOT NULL,
    "mailbox_id"   character varying(32)    NOT NULL REFERENCES "mailbox" ("id") ON DELETE CASCADE,
    "list_key"     text                     NOT NULL,
    "requested_at" timestamp with time zone,
    "method"       character varying(16)    NOT NULL DEFAULT '',
    "failed"       boolean                  NOT NULL DEFAULT false,
    "error"        text                     NOT NULL DEFAULT '',
    PRIMARY KEY ("id")
);

-- One row per list per mailbox: asking again writes the same row.
CREATE UNIQUE INDEX IF NOT EXISTS "mailbox_subscription_key"
    ON "mailbox_subscription" ("mailbox_id", "list_key");
