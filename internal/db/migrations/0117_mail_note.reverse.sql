-- The notes read as mail again, which is how they were before.
UPDATE "mail" SET "kind" = 'outgoing' WHERE "kind" = 'note';
DROP INDEX IF EXISTS "mail_note_identifier";
ALTER TABLE "mail" DROP COLUMN IF EXISTS "note_identifier";
