-- The logo a domain publishes for its own mail.
--
-- One per domain, under the default selector: a sender with one mark has no
-- use for more, and the file is stored the way an uploaded picture is, with
-- the bytes in the file store and this row saying what they are.
--
-- Separate from bimi_logo, which caches what *other* domains publish. The two
-- look alike and mean opposite things: that one is what we fetched, this one
-- is what we serve.
CREATE TABLE IF NOT EXISTS "bimi_publication" (
    "domain_id"   character varying(32)    NOT NULL,
    "file_id"     character varying(32)    NOT NULL,
    "filename"    text                     NOT NULL DEFAULT '',
    "title"       text                     NOT NULL DEFAULT '',
    "created_at"  timestamp with time zone NOT NULL,
    "modified_at" timestamp with time zone NOT NULL,
    PRIMARY KEY ("domain_id")
);

-- Serving one goes the other way: an address names the file, and the row says
-- which domain it belongs to.
CREATE UNIQUE INDEX IF NOT EXISTS "bimi_publication_file" ON "bimi_publication" ("file_id");
