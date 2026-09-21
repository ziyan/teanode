-- The names a domain's MX records point at, when they are not the default.
--
-- The default is one name derived from the domain, "mx." in front of it, and
-- an empty column means exactly that. It is stored only when an operator wants
-- something else: a pair of names, a name that is not "mx", or the server's
-- own name — which is how a domain points at a host in another zone, the
-- arrangement everything defaulted to before this was a choice.
--
-- Comma separated, because it is a short list of host names read as a whole
-- and never queried by its parts. The alternative was a table, which is four
-- more files and a join to answer a question nobody asks.
ALTER TABLE "domain"
    ADD COLUMN "mail_servers" text NOT NULL DEFAULT '';
