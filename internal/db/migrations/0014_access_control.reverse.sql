DROP TABLE "audit_event";
DROP TABLE "group_domain";
DROP TABLE "group_role";
DROP TABLE "user_group";
DROP TABLE "group";
DROP TABLE "role_permission";
DROP TABLE "role";
-- A user without a password cannot go back to a release that required one;
-- they are dropped rather than given a hash that opens nothing.
DELETE FROM "user" WHERE "password_hash" IS NULL OR "password_hash" = '';
ALTER TABLE "user" ALTER COLUMN "password_hash" SET NOT NULL;
ALTER TABLE "user" DROP COLUMN "locale";
ALTER TABLE "user" DROP COLUMN "disabled_at";
DELETE FROM "alias" WHERE "kind" = 'mailbox';
ALTER TABLE "alias" DROP COLUMN "mailbox_id";
