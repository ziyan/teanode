DROP INDEX IF EXISTS "mailbox_subscription_muted";
ALTER TABLE "mailbox_subscription" DROP COLUMN IF EXISTS "muted_at";
ALTER TABLE "mail" DROP COLUMN IF EXISTS "list_stripped";
