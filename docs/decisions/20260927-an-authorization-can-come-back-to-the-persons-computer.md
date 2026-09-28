# An authorization can come back to the person's computer

- Status: accepted
- Date: 2026-09-27
- Deciders: the-owner

## Context

A connected server that asks for OAuth sends the person to its
authorization server, which sends them back with a code to an address the
client named. This server names the dashboard, and the dashboard finishes the
connection.

Some services accept only a loopback address there, the flow a program on
somebody's own machine uses: the program listens on `127.0.0.1`, and the
browser on the same machine brings the code to it. They are built for coding
harnesses and editors, and they refuse any other address. For them, this
server cannot be the place the code comes back to.

The command line already had a way round it for somebody at a terminal:
`teanode agent mcp connect <server> --loopback` listens on the machine it runs
on and finishes the connection itself. A person using the dashboard, or asking
their agent to connect a server, had nothing.

An attached computer is a program on the person's own machine, running as
them, holding a connection to this server. It is the program those services
expect.

## Decision

A server declared with `oauth.redirect: computer` is authorized through the
person's attached computer. When they begin, this server asks the computer to
listen on its loopback interface, and gives the service that loopback address.
The computer answers the one redirect by sending the browser on to the
dashboard address the authorization began from, carrying only the parameters
an authorization comes back with, and closes. The dashboard finishes the
connection as it finishes any other, and the tokens are kept here, sealed, as
they always were.

The computer never reads, keeps or relays the code itself. It passes through
the browser that already carries it. The listener is loopback only, lives at
most fifteen minutes by default, and at most four wait at once.

The choice is the operator's, per server, because it is a fact about the
service (it takes only a loopback address), not a preference of the person's.

## Consequences

The person has to finish the authorization in a browser on the attached
computer, since only that browser can reach its loopback address. A person on
their phone, with the computer at home, cannot. The dashboard says so beside a
server that is authorized this way.

A server authorized this way needs an attached computer to begin, and a
program new enough to offer it; an older one is told to update. Once the
connection is made, the computer is not needed again: the tokens refresh from
here, and the server's tools are called from here, like any other connected
server's.
