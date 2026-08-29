DROP TABLE IF EXISTS "token";
DROP TABLE IF EXISTS "session";

-- Recreated empty. The tokens that were here before the forward migration are
-- not coming back; see the note there.
CREATE TABLE "operator_token" (
    "id" character varying(32) NOT NULL,
    "created_at" timestamp with time zone,
    "modified_at" timestamp with time zone,
    "username" character varying(64) NOT NULL REFERENCES "operator" ("username") ON DELETE CASCADE,
    "name" character varying(128) NOT NULL DEFAULT '',
    "hash" character varying(128) NOT NULL,
    "expires_at" timestamp with time zone,
    PRIMARY KEY ("id")
);

CREATE INDEX "operator_token_username" ON "operator_token" ("username");
