-- How many attachments a message carries, written when it is indexed, so a
-- search can ask for messages with one. Null for messages indexed before
-- this column existed, which no filter matches.
ALTER TABLE "mail" ADD COLUMN "attachment_count" integer;
