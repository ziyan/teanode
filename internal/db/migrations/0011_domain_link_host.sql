-- The name in the addresses this server writes into mail it sends.
--
-- Today that is the pictures in a template, each one an address belonging to a
-- single message. Empty means the domain's first mail server name, which is
-- right when this server answers HTTPS on it — and often it does not. A mail
-- server name resolves to a host whose port 443 may belong to a gateway, a
-- controller, somebody else's site; the mail arrives, and every picture in it
-- is broken. This column is how a domain says where its HTTPS actually is
-- without moving where its mail goes.
ALTER TABLE "domain"
    ADD COLUMN "link_host" text NOT NULL DEFAULT '';
