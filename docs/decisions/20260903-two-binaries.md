# The server and the client are two programs

- Status: accepted
- Date: 2026-09-03
- Deciders: Ziyan Zhou

## Context

`teanode` was one binary: the mail server, with a dashboard compiled in, and
the tool that administers it. The tool's help offered `run` beside `user
list`. A laptop that wanted to list a server's domains downloaded forty
megabytes of server to do it, and there was no build for macOS, because the
server does not run there.

The tool also covered little. A handful of tasks had a command of their own;
everything else went through `teanode api call <Operation> name=value`, which
works but asks the operator to learn the schema to add a domain. Getting a
token onto another machine meant running `teanode token create` on the server
and pasting the secret into an environment variable.

## Decision

Two programs, built from `cmd/teanode-server` and `cmd/teanode`.

**`teanode-server`** is the server and the operations only its own host can
do: `run`, preparing the database (`config init`, `config import|export`), a
development certificate, and recovering accounts when nobody can log in
(`user`, formerly `teanode user --offline`). These are the commands that
write the database directly.

**`teanode`** is the client. Everything it changes goes through the running
server's API, so a change made from a shell is the change the dashboard would
have made. Every resource the API exposes has a command group with `list`,
`get`, `create`, `update` and `delete`, and `teanode api` remains for
whatever is added later. It signs in with `teanode auth login`, which opens
the dashboard in a browser and receives the token over a loopback connection,
and keeps what it learned as a profile per server.

The dividing line is who writes. The client keeps reading the database for
the console path — a token minted from the stored secret, over loopback — so
that on the server's own host nothing has to be set up first; that path was
deliberate (`20260818-the-cli-goes-through-the-api.md`) and stays.

The subcommands live in `internal/cmd` (client) and `internal/cmd/server`
(server) rather than one package, because Go links a whole package: one
package for both sets would have given the client the server's size, which
was the thing being fixed.

## Consequences

The image ships both programs, so `docker compose exec teanode teanode user
list` still administers a server from inside its own container. The release
ships the client for macOS as well as Linux.

An operator upgrading changes the unit and any script that ran `teanode run`,
`teanode config …` or `teanode tls …` to `teanode-server`. `teanode user
--offline` is `teanode-server user`. The token file the client used to read,
`~/.config/teanode/token`, is replaced by profiles; `TEANODE_URL` and
`TEANODE_TOKEN` still work for scripts.

The client's dependency on `internal/configdb` means it links the database
driver. That is a cost in size, paid for keeping the zero-setup console; a
client that could not read the secret would need a token issued before it
could do anything, which is the first-run experience this project refuses to
have.
