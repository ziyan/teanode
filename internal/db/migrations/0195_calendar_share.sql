-- A calendar can be shared with a person or with a circle at a level, and a
-- circle can have a calendar of its own.
--
-- calendar.user_id becomes nullable and calendar.circle_id is added beside it:
-- exactly one of the two is set. A calendar with a circle_id belongs to the
-- circle, is read and written by every member, and goes when the circle goes.
-- A circle has one calendar of each kind at most; only events are ever made
-- for a circle today.
ALTER TABLE "calendar" ALTER COLUMN "user_id" DROP NOT NULL;
ALTER TABLE "calendar" ADD COLUMN "circle_id" character varying(32) REFERENCES "circle" ("id") ON DELETE CASCADE;
ALTER TABLE "calendar" ADD CONSTRAINT "calendar_one_owner" CHECK (num_nonnulls("user_id", "circle_id") = 1);
CREATE UNIQUE INDEX "calendar_one_per_circle" ON "calendar" ("circle_id", "calendar_kind") WHERE "circle_id" IS NOT NULL;

-- One row gives one person, or every member of one circle, a level of access
-- to one calendar. share_level is how much they see and whether they may
-- change it: busy (when, and nothing else), titles (time, title, location),
-- details (everything) or edit (details, and may change the events).
--
-- Sharing with a person needs the sharer and that person to be in a circle
-- together, and sharing with a circle needs the sharer to be a member of it.
-- That is checked when the share is made. What keeps it true afterwards is not
-- this table: a circle's share reaches only members whose circle the calendar's
-- owner is still in, by the join on circle_member, and the code that takes a
-- person out of a circle deletes the direct shares between people who no
-- longer share one.
CREATE TABLE "calendar_share" (
    "id"                 character varying(32)  NOT NULL PRIMARY KEY,
    "calendar_id"        character varying(32)  NOT NULL REFERENCES "calendar" ("id") ON DELETE CASCADE,
    "grantee_user_id"    character varying(32) REFERENCES "user" ("id") ON DELETE CASCADE,
    "grantee_circle_id"  character varying(32) REFERENCES "circle" ("id") ON DELETE CASCADE,
    "share_level"        character varying(16)  NOT NULL
        CHECK ("share_level" IN ('busy', 'titles', 'details', 'edit')),
    "created_at"         timestamp with time zone NOT NULL,
    "created_by_user_id" character varying(32) REFERENCES "user" ("id") ON DELETE SET NULL,
    CHECK (num_nonnulls("grantee_user_id", "grantee_circle_id") = 1),
    UNIQUE ("calendar_id", "grantee_user_id"),
    UNIQUE ("calendar_id", "grantee_circle_id")
);

-- "Which calendars are shared with me" starts from the grantee.
CREATE INDEX "calendar_share_grantee_user" ON "calendar_share" ("grantee_user_id") WHERE "grantee_user_id" IS NOT NULL;
CREATE INDEX "calendar_share_grantee_circle" ON "calendar_share" ("grantee_circle_id") WHERE "grantee_circle_id" IS NOT NULL;
