# The agent reaches a person's computer through a program that person runs, signed in as them, only while they talk

- Status: accepted
- Date: 2026-09-11
- Deciders: Ziyan Zhou

## Context

A personal agent that can read mail and drive a browser still cannot open
the spreadsheet on the person's desk, run the script they wrote, or tell
them how much disk they have left. The natural place for those is the
person's own computer. The server could be given credentials to reach it
— an SSH key, a share — or the computer could reach the server.

## Decision

The computer reaches the server. `teanode computer` is a small program the
person runs on their own machine; it signs in with the person's own token
(the one `teanode auth login` saves, never the server's console), keeps a
websocket open to the server, and answers the agent's requests: a command
through the shell, a file read or written, a directory listed or searched.
It is a device in the same sense as the attached browser tab: attached by
the person, to their own agent, used only by a conversation they are
present in, with its refusals enforced on the device.

The program acts as the person, anywhere on the machine, the way a
terminal of theirs would: nothing is confined and nothing is refused on
their behalf. What stands between the agent and a dangerous command is
the person's yes. One rule over commands (`internal/computer/policy.go`)
says which shapes ask first — removing, moving, installing, sudo,
pushing, ssh, changing what runs on its own, and the graver ones that
take the machine itself, each with its reason on the card — and the rest
run. The filesystem reads freely, writes as a write, deletes only after a
yes. The operator can keep computers off for the whole server.

## Consequences

The server holds no credential for anybody's computer, but a server that
is taken over holds the other end of every attached computer's socket:
the card is the server's, and the program runs what the server sends.
So an attached computer trusts its server the way a terminal trusts the
person at it; the program is theirs to stop, and it is not left running
on a server they do not trust. The other cost is a rule that reads
commands as text — an alias, a script or a variable can hide what a
command does, which is why nothing here replaces the person's eyes on the
conversation.
