-- Going back is refused while a circle has a calendar: it has no account to
-- belong to, and deleting a family's calendar is a decision for a person, not
-- for a rollback. Copy the events out first (each is one file, and
-- "teanode calendar show <id> --json" prints it, or a phone's CalDAV account
-- holds a copy), delete the circle calendars, and then roll back.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM "calendar" WHERE "user_id" IS NULL) THEN
        RAISE EXCEPTION 'a circle has a calendar of its own; copy its events out and delete it before going back';
    END IF;
END
$$;

DROP TABLE "calendar_share";
DROP INDEX "calendar_one_per_circle";
ALTER TABLE "calendar" DROP CONSTRAINT "calendar_one_owner";
ALTER TABLE "calendar" DROP COLUMN "circle_id";
ALTER TABLE "calendar" ALTER COLUMN "user_id" SET NOT NULL;
