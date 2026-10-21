ALTER TABLE "agent_interaction" DROP COLUMN "is_started_by_person";
DELETE FROM "role_permission" WHERE "permission_key" = 'passwords:use';
DROP TABLE "password_agent_key";
DROP TABLE "password_share_key";
DROP TABLE "password_share";
DROP TABLE "password_item_version";
DROP FUNCTION "password_item_version_count_bytes"();
DROP TABLE "password_item";
DROP SEQUENCE "password_item_revision";
DROP TABLE "password_setup";
-- The app mark taken off other apps' tokens by the forward migration is not
-- put back: it should never have been there.
