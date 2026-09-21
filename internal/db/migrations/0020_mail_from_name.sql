-- The display name beside the sender's address, from the From header, so a
-- list can say "Ada Example" without reading the message back. Null for
-- messages that arrived before the column existed.
ALTER TABLE "mail" ADD COLUMN "from_name" varchar(255);
