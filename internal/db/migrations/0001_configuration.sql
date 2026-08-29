-- Configuration moves into the database.
--
-- It lived in teanode.yaml, which one process owned and rewrote. That works
-- for one instance and cannot work for several: two servers behind the same
-- name would each hold their own copy, and whichever wrote last would erase
-- the other's change. Rows in a shared database have one copy, one writer at a
-- time, and every instance sees a change as soon as it is committed.
--
-- domain_id on mail, delivery and the usage tables becomes a real reference
-- again, but deliberately still not a foreign key: a domain can be removed
-- while the mail it received lives on, and reading code treats a missing
-- domain as "deleted" rather than as corruption. Deleting a domain must not
-- delete the record of what it received.

CREATE TABLE "domain" (
    "id" character varying(32) NOT NULL,
    "created_at" timestamp with time zone,
    "modified_at" timestamp with time zone,
    -- The mail domain itself, unique because it is the thing being served.
    "domain" character varying(256) NOT NULL,
    -- The label whose record points at this server, usually "mail".
    "subdomain" character varying(64) NOT NULL DEFAULT '',
    "comment" text NOT NULL DEFAULT '',
    "spam_filter_score_threshold" double precision NOT NULL DEFAULT 5,
    -- The signing key. Secret, and the reason this table is not readable by
    -- anything that does not need to sign mail.
    "dkim_selector" character varying(64) NOT NULL DEFAULT '',
    "dkim_private_key" text NOT NULL DEFAULT '',
    PRIMARY KEY ("id")
);

CREATE UNIQUE INDEX "domain_domain" ON "domain" (lower("domain"));

CREATE TABLE "alias" (
    "id" character varying(32) NOT NULL,
    "created_at" timestamp with time zone,
    "modified_at" timestamp with time zone,
    "domain_id" character varying(32) NOT NULL REFERENCES "domain" ("id") ON DELETE CASCADE,
    -- Ordering within a domain, so the list a reader sees is the list they
    -- arranged rather than whatever the database returns.
    "position" integer NOT NULL DEFAULT 0,
    -- An empty pattern is a catch-all.
    "pattern" text NOT NULL DEFAULT '',
    "comment" text NOT NULL DEFAULT '',
    "kind" character varying(32) NOT NULL DEFAULT '',
    "email" character varying(320) NOT NULL DEFAULT '',
    "webhook" text NOT NULL DEFAULT '',
    "mail_server" text,
    "disabled" boolean NOT NULL DEFAULT false,
    PRIMARY KEY ("id")
);

CREATE INDEX "alias_domain_id" ON "alias" ("domain_id", "position");

CREATE TABLE "credential" (
    "id" character varying(32) NOT NULL,
    "created_at" timestamp with time zone,
    "modified_at" timestamp with time zone,
    "domain_id" character varying(32) NOT NULL REFERENCES "domain" ("id") ON DELETE CASCADE,
    "position" integer NOT NULL DEFAULT 0,
    -- The secret half. The SMTP password is derived from it and the server
    -- secret together, so this table is as sensitive as a password file.
    "key" character varying(64) NOT NULL DEFAULT '',
    "comment" text NOT NULL DEFAULT '',
    -- When set, restricts the credential to sending as that local part.
    "alias" character varying(64) NOT NULL DEFAULT '',
    "disabled" boolean NOT NULL DEFAULT false,
    PRIMARY KEY ("id")
);

CREATE INDEX "credential_domain_id" ON "credential" ("domain_id", "position");

CREATE TABLE "operator" (
    -- The username is the identity, so it is the key. Renaming an account is
    -- deleting one and creating another, which is what it amounts to anyway.
    "username" character varying(64) NOT NULL,
    "created_at" timestamp with time zone,
    "modified_at" timestamp with time zone,
    "password_hash" character varying(128) NOT NULL,
    "email" character varying(320) NOT NULL DEFAULT '',
    PRIMARY KEY ("username")
);

CREATE TABLE "operator_token" (
    "id" character varying(32) NOT NULL,
    "created_at" timestamp with time zone,
    "modified_at" timestamp with time zone,
    -- A token belongs to an account and acts as it, so removing the account
    -- takes its tokens with it. Here that is the database's job.
    "username" character varying(64) NOT NULL REFERENCES "operator" ("username") ON DELETE CASCADE,
    "name" character varying(128) NOT NULL DEFAULT '',
    -- Only the hash. A token is high entropy, so there is nothing a slow
    -- hash would protect against.
    "hash" character varying(128) NOT NULL,
    "expires_at" timestamp with time zone,
    PRIMARY KEY ("id")
);

CREATE INDEX "operator_token_username" ON "operator_token" ("username");

-- Everything that is not a list: server identity, TLS, SMTP behaviour, DNS,
-- the optional integrations, and the secrets they need. One row per section,
-- as JSON, because these are read together and never queried by their parts.
CREATE TABLE "setting" (
    "key" character varying(64) NOT NULL,
    "created_at" timestamp with time zone,
    "modified_at" timestamp with time zone,
    "value" text NOT NULL,
    PRIMARY KEY ("key")
);

-- One row, taken with FOR UPDATE for the length of a change, so that two
-- instances editing configuration at the same moment take turns instead of
-- overwriting each other. The version is what a reader compares to know its
-- copy is stale.
CREATE TABLE "configuration_version" (
    "id" integer NOT NULL DEFAULT 1 CHECK ("id" = 1),
    "version" bigint NOT NULL DEFAULT 0,
    "modified_at" timestamp with time zone,
    PRIMARY KEY ("id")
);

INSERT INTO "configuration_version" ("id", "version", "modified_at") VALUES (1, 0, now());
