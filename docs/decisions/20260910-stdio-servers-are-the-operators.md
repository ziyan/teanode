# A connected server that is a subprocess is declared only by the operator

- Status: accepted
- Date: 2026-09-10
- Deciders: Ziyan Zhou

## Context

The agent can reach tools this server does not have through servers that
speak the Model Context Protocol. Such a server is reached in one of two
ways: over HTTP at a URL, or as a local subprocess whose standard input and
output carry the protocol. The second is how many of them are distributed,
and it is the convenient way to run a small one beside the server.

A subprocess runs as this server's own process, on its host, with whatever
that process can reach: the spool, the database socket, the environment.
Letting a person declare one would let any person with an account run a
command on the server.

## Decision

Servers are declared by the operator, in the configuration, under
`agent.mcp.servers`. A server with a `command` is a subprocess; only the
configuration can name one, and the configuration is the operator's. A
person can connect their own credential or authorize with OAuth to a server
the operator declared, and can never point the server at a command.

## Consequences

An operator who wants a person-specific subprocess declares it once and
marks it user-authenticated; the person connects to it. There is no
self-service for new servers, which is a real limitation for a person who
wants to try a tool the operator has not heard of; the answer is to ask the
operator, which for a subprocess is the right answer.

The subprocess inherits the server's environment merged with the entries the
configuration gives it, and its secrets travel through that environment,
which is redacted where the configuration is shown.
