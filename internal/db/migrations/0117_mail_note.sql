-- A note from a phone's Notes app is kept in the mailbox as a message, and
-- every edit of it is a new message with the same identifier. The column
-- holds that identifier, so the versions of one note are told apart from
-- other notes without reading each message's headers back out of storage.
--
-- Null says the message was stored before this server knew what a note was,
-- and has not been looked at since; empty says it was looked at and is not a
-- note. Every message stored from now on is one or the other, so the pass
-- that finds the notes already here reads only the old ones, and only once.
ALTER TABLE "mail" ADD COLUMN "note_identifier" character varying(64);
CREATE INDEX "mail_note_identifier" ON "mail" ("note_identifier") WHERE "note_identifier" <> '';
