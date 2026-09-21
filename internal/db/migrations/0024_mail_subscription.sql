-- What a message says about the mailing list it came from, extracted when the
-- message is stored because the headers themselves are not in this database:
-- they are in object storage, and a list of subscriptions cannot read every
-- message to build itself.
--
-- list_checked marks a message that has been looked at, whatever was found.
-- Most mail is not subscription mail, so an empty list_key is the ordinary
-- answer and cannot double as "not yet examined".
ALTER TABLE "mail" ADD COLUMN IF NOT EXISTS "list_key" TEXT NOT NULL DEFAULT '';
ALTER TABLE "mail" ADD COLUMN IF NOT EXISTS "list_name" TEXT NOT NULL DEFAULT '';
ALTER TABLE "mail" ADD COLUMN IF NOT EXISTS "list_unsubscribe" TEXT NOT NULL DEFAULT '';
ALTER TABLE "mail" ADD COLUMN IF NOT EXISTS "list_one_click" BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE "mail" ADD COLUMN IF NOT EXISTS "list_checked" BOOLEAN NOT NULL DEFAULT FALSE;

-- Partial, because most mail has no list and there is no reason to index the
-- empty string once per message. Ordered by arrival, which is how a
-- subscription's mail is read.
CREATE INDEX IF NOT EXISTS "mail_list" ON "mail" ("list_key", "received_at" DESC) WHERE "list_key" <> '';

-- The backfill walks the mail a mailbox holds that has not been examined.
CREATE INDEX IF NOT EXISTS "mail_list_unchecked" ON "mail" ("received_at" DESC) WHERE NOT "list_checked";
