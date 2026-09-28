-- A reminders list left behind without its kind would read as a second
-- calendar of events, so the lists go first, with the reminders in them.
DELETE FROM "calendar" WHERE "calendar_kind" = 'reminders';
DROP INDEX IF EXISTS "calendar_one_reminders_list";
ALTER TABLE "calendar" DROP COLUMN IF EXISTS "calendar_kind";
