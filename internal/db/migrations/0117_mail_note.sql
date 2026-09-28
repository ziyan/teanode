-- A note from a phone's Notes app is kept in the mailbox as a message, and
-- every edit of it is a new message with the same identifier. The column
-- holds that identifier, so the versions of one note are told apart from
-- other notes without reading each message's headers back out of storage.
--
-- Null says the message was stored before this server knew what a note was,
-- and has not been looked at since; empty says it was looked at and is not a
-- note. Every message stored from now on is one or the other, so the pass
-- that finds the notes already here reads only the old ones, and only once.
--
-- Written to run again over a database an earlier draft of it already
-- changed: that draft indexed every message with an identifier, which is
-- not what any query asks for.
ALTER TABLE "mail" ADD COLUMN IF NOT EXISTS "note_identifier" character varying(64);
DROP INDEX IF EXISTS "mail_note_identifier";

-- The versions of one note, and every note of a mailbox, are found among the
-- messages of kind note by their identifier.
CREATE INDEX IF NOT EXISTS "mail_note" ON "mail" ("note_identifier") WHERE "kind" = 'note';

-- The pass over old messages walks them by id; once it has been through,
-- this index is empty.
CREATE INDEX IF NOT EXISTS "mail_note_unexamined" ON "mail" ("id") WHERE "note_identifier" IS NULL;
