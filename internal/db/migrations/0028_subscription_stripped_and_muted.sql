-- Two things a subscription can now be: arrived without its way out, and
-- muted.
--
-- list_stripped marks a message whose sender said how to leave and whose
-- List-Unsubscribe was removed before it got here — which is what the relays
-- that hide a reader's address do. The message is a subscription; the way out
-- is missing for a reason worth telling the reader, since it is not the
-- sender withholding it.
ALTER TABLE "mail" ADD COLUMN IF NOT EXISTS "list_stripped" boolean NOT NULL DEFAULT false;

-- Re-open the mail examined under the old rule, so the backfill walks it
-- again. Only the mail that came out with no list: anything already carrying
-- one would cost the same work to reach the same answer.
UPDATE "mail" SET "list_checked" = false WHERE "list_key" = '' AND "list_checked";

-- muted_at is the reader saying they want this list to keep arriving and stop
-- being in the way: it goes to the Archive, read, instead of the Inbox. On the
-- subscription row rather than on a rule, because the reader thinks of it as a
-- property of the list and because this row already outlives the mail.
ALTER TABLE "mailbox_subscription" ADD COLUMN IF NOT EXISTS "muted_at" timestamp with time zone;

-- Delivery asks this for every message that names a list, so it is asked
-- often and answered by one row.
CREATE INDEX IF NOT EXISTS "mailbox_subscription_muted"
    ON "mailbox_subscription" ("mailbox_id", "list_key")
    WHERE "muted_at" IS NOT NULL;
