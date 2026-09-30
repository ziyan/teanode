# The server secret can be kept out of the database

- Status: accepted
- Date: 2026-09-29
- Deciders: the-owner
- Amends: [20260824-configuration-in-the-database.md](20260824-configuration-in-the-database.md)

## Context

The configuration moved into the database with the server secret in it, so
that there was one thing to back up. Since then everything the server stores
that could spend money or sign mail has been sealed with that secret: the
agent's provider keys, the domains' DKIM keys, sources' and skills' secrets,
a connected server's tokens. Every SMTP password is derived from it. With
the secret in the same database, sealing protected nothing from anybody
holding a dump: the dump carries the key beside the locks. Dumps are the
copies that travel, to other disks, other machines, and other people's
hands.

## Decision

The server secret can be kept in a file named at start: `--secret-file` on
`teanode-server`, or `TEANODE_SECRET_FILE`. With one, the configuration rows
hold no secret, only `server.secretCheck`, an HMAC of a fixed label under
the secret, which tells the right file from a wrong one and opens nothing.

- A start whose database still holds the secret, with a file holding the
  same one, removes it from the database. A file holding a different secret
  is refused: the server does not start rather than seal with the wrong key.
- A database with a check and no secret is refused without the file, rather
  than given a new secret.
- A missing file is written only for a server with no secret anywhere. There
  is no command to move an existing server's secret: no deployment needed
  one, and the one server that did was moved by hand.
- Every other secret setting (the session key, the certificate and ACME
  keys, storage keys, an identity provider's client secret) is now sealed as
  the agent's were, so a dump opens nothing at all.
- A migration marks the release. Older releases refuse a database they do not
  recognize, and its reverse refuses to run while the secret is in a file,
  because an older release would read no secret, generate one, and lose
  everything sealed with the old.

Without the flag nothing changes except a warning at start that the secret is
in the database.

## Consequences

There are two things to back up, and they must be kept apart for this to
mean anything. Losing the file loses every sealed key and every SMTP
password, as losing the database did before.

Every instance sharing a database needs the same file.

Dumps taken while the secret was in the database still hold it, so they
have to be deleted. Rotating the secret itself remains impossible without
reissuing every SMTP password.
