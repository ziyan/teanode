-- An invitation that arrived as mail.
--
-- A message may carry a calendar part: an invitation asking somebody to a
-- meeting, a reply from somebody who was asked, or a cancellation. Reading it
-- means fetching the message from storage and decoding it, which is exactly
-- the kind of work that must not happen inside the SMTP transaction -- so
-- delivery writes a row here and a worker does the reading afterwards.
--
-- The row is also what the reader looks up to decide whether to show a
-- message as an invitation, and what an answer is recorded against.
CREATE TABLE "calendar_invitation" (
    "id"          varchar(32)  NOT NULL,
    "user_id"     varchar(32)  NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    "mailbox_id"  varchar(32)  NOT NULL,
    "item_id"     varchar(32)  NOT NULL,
    "mail_id"     varchar(32)  NOT NULL,
    "created_at"  timestamptz  NOT NULL,
    "modified_at" timestamptz  NOT NULL,

    -- waiting: not read yet. read: it was an invitation and the calendar
    -- knows about it. ignored: it carried nothing this server acts on, or it
    -- was refused -- "error" says which.
    "status"     varchar(32) NOT NULL DEFAULT 'waiting',
    "attempts"   int         NOT NULL DEFAULT 0,
    "not_before" timestamptz,
    "claimed_at" timestamptz,
    "claimed_by" varchar(64) NOT NULL DEFAULT '',
    "error"      text        NOT NULL DEFAULT '',

    -- What it turned out to be. The method is REQUEST, REPLY or CANCEL; the
    -- identifier is the event's, which is how a reply is matched to what it
    -- answers even when it arrives years later from a program that kept
    -- nothing else.
    "method"      varchar(32)  NOT NULL DEFAULT '',
    "uid"         varchar(255) NOT NULL DEFAULT '',
    "sequence"    int          NOT NULL DEFAULT 0,
    "organizer"   varchar(320) NOT NULL DEFAULT '',
    "calendar_id" varchar(32),
    "object_id"   varchar(255),

    PRIMARY KEY ("id")
);

-- One row per message: a message redelivered is the same invitation, and two
-- rows would mean reading it twice and answering it twice.
CREATE UNIQUE INDEX "calendar_invitation_item" ON "calendar_invitation" ("mailbox_id", "item_id");

-- What the worker claims: the waiting ones, oldest first.
CREATE INDEX "calendar_invitation_waiting" ON "calendar_invitation" ("status", "not_before");

-- What the reader asks: is this message an invitation?
CREATE INDEX "calendar_invitation_mail" ON "calendar_invitation" ("mail_id");
