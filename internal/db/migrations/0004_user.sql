-- The accounts that administer this server, keyed by an identifier rather
-- than by the name somebody signs in with.
--
-- "operator" was the table and the username was the key, on the reasoning that
-- an account is keyed by its username in the configuration and that renaming
-- one was not something the dashboard offered. It offers it now, and a key
-- that changes is not a key: every session and every API token named an
-- account by a string that a rename would have invalidated, so the rename had
-- to go and rewrite them, and anything that referenced an account in future
-- would have had to do the same.
--
-- So: an identifier that never changes, and the username as an ordinary
-- unique column. Sessions and tokens point at the identifier, which is why
-- they no longer have to be touched when somebody renames themselves.
--
-- "user" is a reserved word in PostgreSQL and has to be quoted. It is quoted
-- everywhere in this project already — every identifier is — and the name
-- being the obvious one is worth the quoting.

CREATE TABLE "user" (
    -- A lowercase ULID, like every other identifier here: sortable by when it
    -- was made, and safe in a URL without escaping.
    "id" character varying(32) NOT NULL,
    "created_at" timestamp with time zone,
    "modified_at" timestamp with time zone,

    -- What they sign in with. Unique without regard to case, because two
    -- accounts differing only in case are two accounts nobody can tell apart
    -- on a sign-in form.
    "username" character varying(64) NOT NULL,

    -- What to call this person, when they have said. Optional; the username
    -- stands in.
    "name" character varying(128) NOT NULL DEFAULT '',

    "password_hash" character varying(128) NOT NULL,
    "email" character varying(320) NOT NULL DEFAULT '',
    PRIMARY KEY ("id")
);

CREATE UNIQUE INDEX "user_username" ON "user" (lower("username"));

-- A ULID for each existing account, built here because a migration is SQL and
-- there is nowhere else to build one. Ten characters of millisecond timestamp
-- most significant first, then sixteen more, over Crockford's base32 alphabet
-- — which is the shape security.NewULID produces, lowercased.
--
-- The second half comes from an md5 of the username rather than from random():
-- an uncorrelated subquery is an InitPlan, which PostgreSQL evaluates once and
-- reuses for every row, so a random suffix came out the same for all of them
-- and the primary key collided. This one names the row it belongs to, so it
-- has to be computed per row — and hex digits are all valid Crockford
-- characters, so no mapping is needed.
INSERT INTO "user" ("id", "created_at", "modified_at", "username", "password_hash", "email")
SELECT
    (
        SELECT string_agg(
            substr(
                '0123456789abcdefghjkmnpqrstvwxyz',
                (((floor(extract(epoch from coalesce("operator"."created_at", now())) * 1000)::bigint
                    >> (place * 5)) & 31)::int) + 1,
                1
            ),
            '' ORDER BY place DESC
        )
        FROM generate_series(0, 9) AS place
    ) || substr(md5("operator"."username" || clock_timestamp()::text), 1, 16),
    "operator"."created_at",
    "operator"."modified_at",
    "operator"."username",
    "operator"."password_hash",
    "operator"."email"
FROM "operator";

-- Sessions and tokens follow the account rather than the name.
ALTER TABLE "session" ADD COLUMN "user_id" character varying(32);
ALTER TABLE "token" ADD COLUMN "user_id" character varying(32);

UPDATE "session" SET "user_id" = "user"."id" FROM "user" WHERE "user"."username" = "session"."username";
UPDATE "token" SET "user_id" = "user"."id" FROM "user" WHERE "user"."username" = "token"."username";

-- A session or token naming an account that no longer exists authenticates
-- nobody, so there is nothing to keep.
DELETE FROM "session" WHERE "user_id" IS NULL;
DELETE FROM "token" WHERE "user_id" IS NULL;

DROP INDEX IF EXISTS "idx_session_username";
DROP INDEX IF EXISTS "idx_token_username";
ALTER TABLE "session" DROP COLUMN "username";
ALTER TABLE "token" DROP COLUMN "username";

ALTER TABLE "session" ALTER COLUMN "user_id" SET NOT NULL;
ALTER TABLE "token" ALTER COLUMN "user_id" SET NOT NULL;

-- Removing an account takes its sessions and tokens with it. Here that is the
-- database's job rather than something every delete has to remember.
ALTER TABLE "session"
    ADD CONSTRAINT "session_user_id" FOREIGN KEY ("user_id") REFERENCES "user" ("id") ON DELETE CASCADE;
ALTER TABLE "token"
    ADD CONSTRAINT "token_user_id" FOREIGN KEY ("user_id") REFERENCES "user" ("id") ON DELETE CASCADE;

CREATE INDEX "idx_session_user_id" ON "session" ("user_id");
CREATE INDEX "idx_token_user_id" ON "token" ("user_id");

DROP TABLE "operator";

-- "setting" holds each configuration section as a YAML document, keyed by the
-- section's name — everything that is not a list of its own, which is to say
-- everything but the domains, the aliases, the credentials and the accounts.
-- It is the configuration; "setting" made it sound like a bag of switches
-- beside one.
ALTER TABLE "setting" RENAME TO "configuration";
