-- The address book is the only list of people.
--
-- Every address that ever wrote to a mailbox was kept in a row of its own,
-- with a count and a last-seen time: a ledger of everybody who has ever
-- written, built without anybody asking for it. It fed four things -- the
-- "sender is known" rule condition, the composer's completion, the agent's
-- search for an address, and the out-of-office reply's memory of who it had
-- already answered.
--
-- Three of those now read the address book, which is the list the person
-- actually keeps. The fourth, "once a week per sender", goes: it is the only
-- thing that needed a row per address, and a mail server that keeps a list of
-- everyone who has written to it in order to not write back twice is keeping
-- the wrong thing.
--
-- What the out-of-office still needs is the loop guard: fifty replies an hour
-- per mailbox, which is what stops two away messages talking to each other
-- for ever. That is a count per hour, and needs no addresses at all.
DROP TABLE IF EXISTS "mailbox_contact";

CREATE TABLE "mailbox_auto_reply" (
    "mailbox_id" character varying(32)    NOT NULL REFERENCES "mailbox" ("id") ON DELETE CASCADE,
    "hour"       timestamp with time zone NOT NULL,
    "count"      integer                  NOT NULL DEFAULT 0,
    PRIMARY KEY ("mailbox_id", "hour")
);
