DELETE FROM "role_permission" WHERE "permission_key" = 'passwords:use';
DROP TABLE "password_agent_key";
DROP TABLE "password_share_key";
DROP TABLE "password_share";
DROP TABLE "password_item_version";
DROP TABLE "password_item";
DROP SEQUENCE "password_item_revision";
DROP TABLE "password_setup";
