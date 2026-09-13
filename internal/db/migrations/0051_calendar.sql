-- A person's calendar, and the events in it.
--
-- The calendar belongs to the account rather than to a mailbox, exactly as
-- the address book does: somebody with two mailboxes has one calendar and
-- reaches it through either. Signing in from a phone is still per mailbox,
-- with one of its app passwords, and the calendar a client then sees is the
-- one belonging to the account that owns that mailbox.
CREATE TABLE "calendar" (
    "id"          varchar(32)  NOT NULL,
    "user_id"     varchar(32)  NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    "created_at"  timestamptz  NOT NULL,
    "modified_at" timestamptz  NOT NULL,
    "name"        varchar(200) NOT NULL DEFAULT '',
    "description" text         NOT NULL DEFAULT '',
    "colour"      varchar(16)  NOT NULL DEFAULT '',
    "time_zone"   varchar(64)  NOT NULL DEFAULT '',
    PRIMARY KEY ("id")
);

CREATE INDEX "calendar_user" ON "calendar" ("user_id");

-- The iCalendar text is the event. Everything beside it is pulled out when
-- the object is written, so that listing and searching never have to parse
-- iCalendar; where the two disagree the text is right.
--
-- One row is one file, which is usually one event but may be an event
-- together with the occurrences of it that were changed one at a time. They
-- share a UID, which is why the UID is unique here and the identifier is not
-- the same thing.
--
-- "id" is the file name the client chose, so it is unique only inside one
-- calendar, and the key is both columns together. It is wide because clients
-- name a file after its UID and a UID is commonly a 36-character one -- a
-- narrower column refused every event an iPhone ever made.
--
-- "etag" names a version. A client sends it back in an If-Match header, and a
-- write whose ETag is stale is refused, so two devices editing at once cannot
-- silently overwrite one another.
CREATE TABLE "calendar_object" (
    "id"          varchar(255) NOT NULL,
    "calendar_id" varchar(32)  NOT NULL REFERENCES "calendar" ("id") ON DELETE CASCADE,
    "created_at"  timestamptz  NOT NULL,
    "modified_at" timestamptz  NOT NULL,
    "uid"         varchar(255) NOT NULL,
    "etag"        varchar(64)  NOT NULL,
    "data"        text         NOT NULL,
    "summary"     varchar(512) NOT NULL DEFAULT '',
    "location"    varchar(512) NOT NULL DEFAULT '',
    "starts_at"   timestamptz,
    "ends_at"     timestamptz,
    "all_day"     boolean      NOT NULL DEFAULT false,
    "recurring"   boolean      NOT NULL DEFAULT false,
    "status"      varchar(32)  NOT NULL DEFAULT '',
    PRIMARY KEY ("calendar_id", "id")
);

CREATE UNIQUE INDEX "calendar_object_uid" ON "calendar_object" ("calendar_id", "uid");
CREATE INDEX "calendar_object_when" ON "calendar_object" ("calendar_id", "starts_at");

-- When the events actually happen, one row per occurrence.
--
-- This is derived and never authoritative: it is rebuilt from the iCalendar
-- text whenever an object is written, in the same transaction, and anything
-- here that disagrees with the text is this table being wrong. It exists
-- because the two questions asked most often -- "what is on this week" and
-- "when is this person busy" -- are about a window, and answering them from
-- the text means decoding every event in the calendar and expanding its
-- recurrence rule, which is work proportional to the calendar rather than to
-- the window.
--
-- An event that recurs forever cannot be indexed forever, so what is written
-- here reaches a horizon; past it, the text is consulted.
CREATE TABLE "calendar_occurrence" (
    "calendar_id" varchar(32)  NOT NULL,
    "object_id"   varchar(255) NOT NULL,
    "starts_at"   timestamptz  NOT NULL,
    "ends_at"     timestamptz  NOT NULL,
    "all_day"     boolean      NOT NULL DEFAULT false,
    PRIMARY KEY ("calendar_id", "object_id", "starts_at"),
    FOREIGN KEY ("calendar_id", "object_id")
        REFERENCES "calendar_object" ("calendar_id", "id") ON DELETE CASCADE
);

CREATE INDEX "calendar_occurrence_window" ON "calendar_occurrence" ("calendar_id", "starts_at", "ends_at");

-- Keeping a calendar is something a member does with their own account, like
-- having a mailbox, so everybody who can read mail here gets it. An operator
-- can take it away from a role in the usual place.
INSERT INTO "role_permission" ("role_id", "permission_key")
SELECT "id", 'calendar:use' FROM "role" WHERE lower("name") IN ('administrator', 'operator', 'member')
ON CONFLICT DO NOTHING;
