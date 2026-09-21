-- The logo a sending domain publishes for its mail.
--
-- BIMI is a TXT record naming an SVG, and the SVG is on the sender's server.
-- Fetching it when a message is opened would tell that sender which address
-- read which message at what moment, which is the thing the remote image
-- proxy exists to prevent — so it is fetched here, once, and cached.
--
-- Keyed by domain and selector, which is what the record is keyed by. The
-- content may be null: a domain that publishes no record, or one whose logo
-- could not be fetched, is worth remembering so it is not asked again for a
-- day.
CREATE TABLE IF NOT EXISTS "bimi_logo" (
    "domain"       text                     NOT NULL,
    "selector"     text                     NOT NULL,
    "checked_at"   timestamp with time zone NOT NULL,
    "logo_url"     text                     NOT NULL DEFAULT '',
    "content_type" character varying(64)    NOT NULL DEFAULT '',
    "content"      bytea,
    "error"        text                     NOT NULL DEFAULT '',
    PRIMARY KEY ("domain", "selector")
);
