-- Back to an account keyed by its username.
--
-- Sessions and tokens whose account has since been renamed come back pointing
-- at the new name, which is the only name that exists.

CREATE TABLE "operator" (
    "username" character varying(64) NOT NULL,
    "created_at" timestamp with time zone,
    "modified_at" timestamp with time zone,
    "password_hash" character varying(128) NOT NULL,
    "email" character varying(320) NOT NULL DEFAULT '',
    PRIMARY KEY ("username")
);

INSERT INTO "operator" ("username", "created_at", "modified_at", "password_hash", "email")
SELECT "username", "created_at", "modified_at", "password_hash", "email" FROM "user";

ALTER TABLE "session" ADD COLUMN "username" character varying(256);
ALTER TABLE "token" ADD COLUMN "username" character varying(256);

UPDATE "session" SET "username" = "user"."username" FROM "user" WHERE "user"."id" = "session"."user_id";
UPDATE "token" SET "username" = "user"."username" FROM "user" WHERE "user"."id" = "token"."user_id";

DROP INDEX IF EXISTS "idx_session_user_id";
DROP INDEX IF EXISTS "idx_token_user_id";
ALTER TABLE "session" DROP CONSTRAINT IF EXISTS "session_user_id";
ALTER TABLE "token" DROP CONSTRAINT IF EXISTS "token_user_id";
ALTER TABLE "session" DROP COLUMN "user_id";
ALTER TABLE "token" DROP COLUMN "user_id";

ALTER TABLE "session" ALTER COLUMN "username" SET NOT NULL;
ALTER TABLE "token" ALTER COLUMN "username" SET NOT NULL;

CREATE INDEX "idx_session_username" ON "session" ("username");
CREATE INDEX "idx_token_username" ON "token" ("username");

DROP TABLE "user";

ALTER TABLE "configuration" RENAME TO "setting";
