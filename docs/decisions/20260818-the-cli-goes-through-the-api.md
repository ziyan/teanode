# The command line tool goes through the API, not the file

- Status: accepted; the tool is now the `teanode` client of
  `20260903-two-binaries.md`, which keeps every decision here
- Date: 2026-08-18
- Deciders: Ziyan Zhou

## Context

Configuration lives in one writable YAML file
(`20260818-configuration-in-yaml.md`). The server holds the whole of it in
memory and rewrites the file from that copy whenever anything changes through
the dashboard.

The command line tool opened the same file and wrote to it directly. With the
server running, that is two writers and no lock. `teanode credential add`
appends a credential to the file; the operator then adds a domain in the
dashboard; the server writes its in-memory copy back and the credential is
gone. Nothing errors, nothing logs, and the credential simply stops existing.
The window is not narrow — it lasts until the next dashboard change, which may
be days.

There is also a plainer problem: the tool only worked while sitting on the
server, because it needed the file.

## Decision

Anything that changes configuration goes through the running server's GraphQL
endpoint. The server is the only writer.

The tool authenticates two ways:

**On the server itself**, it reads the configuration file for the server
secret, mints a short lived token signed with it, and connects over loopback.
Nothing has to be set up first. This is not an escalation: minting one requires
reading the file, and whoever can read it can already read the session key, the
signing keys and every password hash, or simply edit the file.

**From anywhere else**, with `--url` and a token from `teanode token create`,
kept in `TEANODE_TOKEN` or `~/.config/teanode/token`. A token belongs to an
account and acts as that person, so removing the account revokes its tokens in
the same step — which is why tokens are stored inside the account rather than
in a list of their own.

Two things stay off this path:

- **Reads** may fall back to the file when the server is not running, because a
  read cannot lose anybody's change and the file is current either way. Without
  that, `teanode dkim show` could not print a DNS record before the server had
  ever started, which is exactly when it is needed.
- **`--offline`** on `teanode user` edits the file directly, for a server that
  will not start or that nobody can log into. It refuses when the server is
  reachable, which is the case it exists to avoid.

`teanode config init`, `validate` and `show` are file operations by nature and
never talk to a server.

## Consequences

The dashboard and the command line do the same thing through the same code, so
a change made either way behaves identically and is validated identically.

The tool administers a remote server, which it could not before.

A write now fails when the server is down, where it used to succeed. The error
says so and names the two ways forward. This is the intended trade: a refused
write is better than one that is accepted and later discarded.
