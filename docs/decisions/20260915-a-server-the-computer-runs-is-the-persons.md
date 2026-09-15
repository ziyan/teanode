# A connected server that runs on the person's own computer is the person's

- Status: accepted
- Date: 2026-09-15
- Deciders: Ziyan Zhou
- Amends: [20260910-stdio-servers-are-the-operators.md](20260910-stdio-servers-are-the-operators.md)

## Context

The earlier decision says a connected server spoken to over a command is
declared only by the operator, because the command runs on this server, as
this server's own process, with everything that process can reach. That
reasoning is right and stands.

An attached computer changed what a command can mean. A person who has run
`teanode computer` already lets their agent run commands on that machine,
as them, from a conversation they are present in. A connected server that
runs *there* is no more than one of those commands, held open: the same
machine, the same person, the same rule that they are asked and are present.
Nothing this server holds is reachable from it.

## Decision

A command-spoken server may be declared with `location: computer`. Its
command then runs on the person's own attached computer, as them, and its
standard input and output reach this server through a session. Such a
server is offered only while that person has a computer attached, never to a
run with nobody present, and the configuration refuses to mark it headless.

The declaration still lives in `agent.mcp.servers`, which is the operator's,
because that is where every server is declared and a second place to look
is a way to miss one. What the operator is declaring is different in kind,
though: not "run this on my server" but "this may run on a person's machine
when they attach one", which commits nothing of the operator's.

## Consequences

A person can have a server of their own -- a tool that reads their files,
speaks to hardware on their desk, or needs their own signed-in state --
without the operator installing anything on the server or trusting it with
the server's own process. The operator declares the name and the command;
the person attaches the computer that has it.

The protocol is unchanged. The transport speaks over a session's pipes
instead of a subprocess's, and everything above the transport cannot tell.

A server on the computer is as available as the computer is. It is gone
when the person stops the program, and comes back when they start it, which
is what they would expect of anything running on their own machine.
