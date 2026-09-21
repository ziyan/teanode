-- Files an operator uploaded to put in a template: a logo, a header, an image
-- in a signature.
--
-- The bytes are not here. They go where the messages go — the spool directory,
-- mirrored to the object store when one is configured — because a database
-- holding every picture anybody ever uploaded is a database that is slow to
-- back up and slower to restore, and none of these bytes is ever queried.
-- This row is what says which file it is, whose it is, and what to answer
-- with when a mail program asks for it.
--
-- A picture belongs to one domain. It is served from that domain's own mail
-- host, and may only be put in that domain's templates, so that a message
-- carries no name but the one it was sent from.
CREATE TABLE "media" (
    "id" character varying(32) NOT NULL,
    "created_at" timestamp with time zone,
    "modified_at" timestamp with time zone,
    "domain_id" character varying(255) NOT NULL DEFAULT '',
    "filename" character varying(255) NOT NULL DEFAULT '',
    "content_type" character varying(128) NOT NULL DEFAULT '',
    "size" bigint NOT NULL DEFAULT 0,
    "checksum" character varying(64) NOT NULL DEFAULT '',
    PRIMARY KEY ("id")
);

-- Listing what a domain has, newest first, is the only query this table
-- answers that is not by identifier.
CREATE INDEX "media_domain_id_created_at" ON "media" ("domain_id", "created_at" DESC);
