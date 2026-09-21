DROP INDEX IF EXISTS "mail_list_unchecked";
DROP INDEX IF EXISTS "mail_list";
ALTER TABLE "mail" DROP COLUMN IF EXISTS "list_checked";
ALTER TABLE "mail" DROP COLUMN IF EXISTS "list_one_click";
ALTER TABLE "mail" DROP COLUMN IF EXISTS "list_unsubscribe";
ALTER TABLE "mail" DROP COLUMN IF EXISTS "list_name";
ALTER TABLE "mail" DROP COLUMN IF EXISTS "list_key";
