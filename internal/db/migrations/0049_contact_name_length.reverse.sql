-- Contacts whose identifier no longer fits are not the reverse migration's to
-- throw away: it fails rather than silently losing them, and an operator who
-- means to go back deletes them first.
ALTER TABLE "contact" ALTER COLUMN "id" TYPE varchar(32);
