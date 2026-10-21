-- What a web page says about itself, for the card shown under a message that
-- holds a link.
--
-- The page is fetched by this server and the answer kept, so the page's site
-- learns that this server looked and not who was reading. It is the same for
-- everyone who sees the link: what is cached is what an anonymous visitor
-- gets, never anything of a person's.
--
-- url is the link with its tracking parameters and fragment taken off, which
-- is how two messages that link to one page come to share one row. The page's
-- picture is not here: the bytes are in the file store under image_file_id,
-- scaled down, and the picture is served from this server. image_file_id is
-- empty when the page had no usable picture.
--
-- A row is a successful read, or the failure of one. fetch_failure is empty
-- for a page that was read and a short word for why when it could not be. A
-- failure is kept too, or every message that holds the link would ask the page
-- again each time it is drawn. How long a row is trusted is decided when it is
-- read: a day for a page, an hour for a failure.
CREATE TABLE "link_preview" (
    "url"                 text                     NOT NULL PRIMARY KEY,
    "title"               text                     NOT NULL DEFAULT '',
    "description"         text                     NOT NULL DEFAULT '',
    "site_name"           text                     NOT NULL DEFAULT '',
    "image_file_id"       character varying(128)   NOT NULL DEFAULT '',
    "image_size_bytes"    integer                  NOT NULL DEFAULT 0,
    "image_width_pixels"  integer                  NOT NULL DEFAULT 0,
    "image_height_pixels" integer                  NOT NULL DEFAULT 0,
    "fetched_at"          timestamp with time zone NOT NULL,
    "fetch_failure"       text                     NOT NULL DEFAULT ''
);

-- A picture is served by the name of its file, and the row says which page it
-- belongs to.
CREATE UNIQUE INDEX "link_preview_image_file" ON "link_preview" ("image_file_id") WHERE "image_file_id" <> '';

-- Rows fetched more than a month ago are removed, oldest first.
CREATE INDEX "link_preview_fetched_at" ON "link_preview" ("fetched_at");
