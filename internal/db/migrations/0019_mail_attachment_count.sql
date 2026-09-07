-- How many attachments a message carries, written when it is indexed, so a
-- search can ask for messages with one. Null for messages indexed before
-- this column existed: "with attachment" leaves them out, "without" counts
-- them as having none.
ALTER TABLE "mail" ADD COLUMN "attachment_count" integer;
