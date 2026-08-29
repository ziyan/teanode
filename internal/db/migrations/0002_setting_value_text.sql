-- The setting value is text, not jsonb.
--
-- It holds a JSON document either way, and nothing queries inside it: a
-- setting is read and written whole. What jsonb adds is a restriction — a
-- jsonb string cannot contain a NUL, and PostgreSQL refuses the escape that
-- JSON encoding produces for one.
--
-- The server secret is raw bytes. A deployment whose secret happens to
-- contain a zero byte could not store its own configuration, and the failure
-- came out as "unsupported Unicode escape sequence", a long way from anything
-- an operator could act on.
ALTER TABLE "setting" ALTER COLUMN "value" TYPE text;

-- Same reasoning: an alias's mail server credentials are stored here, and a
-- password is whatever somebody chose.
ALTER TABLE "alias" ALTER COLUMN "mail_server" TYPE text;
