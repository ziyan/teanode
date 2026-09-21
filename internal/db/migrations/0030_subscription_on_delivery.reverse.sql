DROP INDEX IF EXISTS "mailbox_item_subscription";
ALTER TABLE "mailbox_item" DROP COLUMN IF EXISTS "subscription_id";

-- Back to a row meaning "something was asked of this list". What the reader
-- did is kept; the rows that only say a list exists are what this adds, and
-- they are what it takes away.
DELETE FROM "mailbox_subscription"
WHERE "requested_at" IS NULL
  AND "muted_at" IS NULL
  AND "images_at" IS NULL;
