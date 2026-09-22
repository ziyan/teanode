-- Authorized programs lose their tokens and their registrations. A token a
-- person minted by hand is untouched, because it has no client and no
-- resource; anything a program was authorized to hold stops working, and the
-- program has to be authorized again after an upgrade.
DELETE FROM "token" WHERE "client_id" IS NOT NULL;

DROP INDEX IF EXISTS "idx_token_client_id";
ALTER TABLE "token" DROP COLUMN "refresh_hash";
ALTER TABLE "token" DROP COLUMN "resource";
ALTER TABLE "token" DROP COLUMN "client_id";

DROP TABLE "oauth_authorization";
DROP TABLE "oauth_client";
