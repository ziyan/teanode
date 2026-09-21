-- A TLS certificate for each domain's own mail server name.
--
-- Every domain's MX now names a host in that domain — mx.example.com rather
-- than a name belonging to whichever domain the server is called after — so a
-- sender connecting to it should be handed a certificate for the name it
-- asked for. Until now there was one certificate for the whole server, and
-- every sender was handed a name belonging to somebody else's domain.
--
-- Kept here rather than in files beside the configuration for the same reason
-- the signing key is: restoring a configuration restores a working server,
-- certificate included. The private half is encrypted before it is written,
-- under its own label, so that reading this column is not enough to serve
-- traffic as this domain.
--
-- Empty is the normal state and not an error: the server serves its own
-- certificate for that name until one has been obtained.
ALTER TABLE "domain"
    ADD COLUMN "certificate" text NOT NULL DEFAULT '',
    ADD COLUMN "certificate_private_key" text NOT NULL DEFAULT '';
