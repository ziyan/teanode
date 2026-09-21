-- Who may do what, and who did what.
--
-- Domains, aliases, credentials and accounts were rows already, but they were
-- read into one configuration document and written back whole on every
-- change. From here they are rows like any other: read when needed, changed
-- one at a time in a transaction, and each change recorded in the audit log.
-- The configuration table keeps only what is configuration: the server's
-- settings, one section per row.

-- Rows are no longer rewritten wholesale, so the timestamps mean something
-- and are required.
UPDATE "domain" SET "created_at" = now() WHERE "created_at" IS NULL;
UPDATE "domain" SET "modified_at" = "created_at" WHERE "modified_at" IS NULL;
UPDATE "alias" SET "created_at" = now() WHERE "created_at" IS NULL;
UPDATE "alias" SET "modified_at" = "created_at" WHERE "modified_at" IS NULL;
UPDATE "credential" SET "created_at" = now() WHERE "created_at" IS NULL;
UPDATE "credential" SET "modified_at" = "created_at" WHERE "modified_at" IS NULL;
UPDATE "user" SET "created_at" = now() WHERE "created_at" IS NULL;
UPDATE "user" SET "modified_at" = "created_at" WHERE "modified_at" IS NULL;

-- An alias of kind "mailbox" delivers into a mailbox on this server.
ALTER TABLE "alias" ADD COLUMN "mailbox_id" character varying(32) NOT NULL DEFAULT '';

-- A disabled user cannot sign in and keeps their mail; a user with no
-- password signs in with a passkey or through an identity provider.
ALTER TABLE "user"
    ADD COLUMN "disabled_at" timestamp with time zone,
    ADD COLUMN "locale" character varying(16) NOT NULL DEFAULT '';
ALTER TABLE "user" ALTER COLUMN "password_hash" DROP NOT NULL;

CREATE TABLE "role" (
    "id"          character varying(32)  NOT NULL,
    "created_at"  timestamp with time zone NOT NULL,
    "modified_at" timestamp with time zone NOT NULL,
    "name"        character varying(128) NOT NULL,
    "description" text                   NOT NULL DEFAULT '',
    PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "role_name" ON "role" (lower("name"));

-- The permission is a plain key: the vocabulary is Go, and a row naming a
-- permission the code has forgotten is ignored rather than fatal.
CREATE TABLE "role_permission" (
    "role_id"        character varying(32) NOT NULL REFERENCES "role" ("id") ON DELETE CASCADE,
    "permission_key" character varying(64) NOT NULL,
    PRIMARY KEY ("role_id", "permission_key")
);

CREATE TABLE "group" (
    "id"          character varying(32)  NOT NULL,
    "created_at"  timestamp with time zone NOT NULL,
    "modified_at" timestamp with time zone NOT NULL,
    "name"        character varying(128) NOT NULL,
    "description" text                   NOT NULL DEFAULT '',
    -- The group's name at the identity provider, when single sign-on fills it.
    "idp_group"   character varying(256) NOT NULL DEFAULT '',
    PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "group_name" ON "group" (lower("name"));

-- Membership: the many-to-many between users and groups, nothing more.
CREATE TABLE "user_group" (
    "user_id"  character varying(32) NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    "group_id" character varying(32) NOT NULL REFERENCES "group" ("id") ON DELETE CASCADE,
    PRIMARY KEY ("user_id", "group_id")
);
CREATE INDEX "user_group_group" ON "user_group" ("group_id");

-- A group's roles: every user in the group holds them.
CREATE TABLE "group_role" (
    "group_id" character varying(32) NOT NULL REFERENCES "group" ("id") ON DELETE CASCADE,
    "role_id"  character varying(32) NOT NULL REFERENCES "role" ("id") ON DELETE CASCADE,
    PRIMARY KEY ("group_id", "role_id")
);

-- A group's domains: the ones its roles' domain-kind permissions apply to.
-- All-domains permissions do not look here.
CREATE TABLE "group_domain" (
    "group_id"  character varying(32) NOT NULL REFERENCES "group" ("id") ON DELETE CASCADE,
    "domain_id" character varying(32) NOT NULL REFERENCES "domain" ("id") ON DELETE CASCADE,
    PRIMARY KEY ("group_id", "domain_id")
);
CREATE INDEX "group_domain_domain" ON "group_domain" ("domain_id");

-- One row per administrative change, written in the transaction that made
-- it. No foreign keys: the row outlives the user who made the change and the
-- row it describes, which is the point of keeping it.
CREATE TABLE "audit_event" (
    "id"            character varying(32) NOT NULL,
    "created_at"    timestamp with time zone NOT NULL,
    "actor_kind"    character varying(8)  NOT NULL,
    "actor_user_id" character varying(32) NOT NULL DEFAULT '',
    "token_id"      character varying(32) NOT NULL DEFAULT '',
    "source_ip"     character varying(45) NOT NULL DEFAULT '',
    "instance"      character varying(64) NOT NULL DEFAULT '',
    "resource_type" character varying(32) NOT NULL,
    "resource_id"   character varying(64) NOT NULL,
    "action"        character varying(8)  NOT NULL,
    "before"        jsonb,
    "after"         jsonb,
    PRIMARY KEY ("id")
);
CREATE INDEX "audit_event_resource" ON "audit_event" ("resource_type", "resource_id", "created_at" DESC);
CREATE INDEX "audit_event_actor" ON "audit_event" ("actor_user_id", "created_at" DESC) WHERE "actor_user_id" <> '';
CREATE INDEX "audit_event_time" ON "audit_event" ("created_at" DESC);
