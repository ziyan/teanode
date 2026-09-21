DELETE FROM "role_permission" WHERE "permission_key" IN ('agent:use', 'agent:audit');

ALTER TABLE "user" DROP COLUMN IF EXISTS "locale_seen";
ALTER TABLE "user" DROP COLUMN IF EXISTS "timezone_seen_at";
ALTER TABLE "user" DROP COLUMN IF EXISTS "timezone_mode";
ALTER TABLE "user" DROP COLUMN IF EXISTS "timezone";

DROP TABLE IF EXISTS "agent_usage";
DROP TABLE IF EXISTS "agent_job";

ALTER TABLE "mailbox" DROP COLUMN IF EXISTS "agent";

DROP TABLE IF EXISTS "agent";
