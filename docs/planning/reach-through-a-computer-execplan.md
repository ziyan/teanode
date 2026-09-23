# A service is reached through the person's computer

## Why this matters

A skill or a connected server talks to one service, and some services answer
only inside one network: a code host behind a company VPN, a home automation
box on the home network. The server cannot reach either. The person's own
computer, attached with `teanode computer`, can, and since the change that
added the `http` action to the computer program it can make a request on the
agent's behalf. What is missing is a way to say which service goes through
which computer, and for the agent to choose one for a single call.

After this change the person sets a reach for each skill and each connected
server: what its requests go through, this server or one of their computers
by name. The
agent can name a different computer for one call, and is asked first when it
does. A connected server that runs as a command on the person's computer runs
on the one they chose, rather than whichever computer happened to be first.

## Where things are

`internal/computer/http.go` makes a request on the computer.
`internal/agent/tools/computer/http.go` wraps that as an `http.Client`.
Skills become tools in `internal/agent/tools_skill.go`; their HTTP steps take
the client from `skills.Running.Client`, so a skill goes through a computer
when that client does.
Connected servers become tools in `internal/agent/tools_mcp.go`: `transportFor`
builds the transport, and `connection` caches one session per server and
person. The person's settings live on the Connections tab in
`web/src/pages/agent.tsx`.

## Words

One word per idea, in the code and in what people read. A **reach** is the
setting: which computer a skill's or server's requests go through. Requests go
**through** a computer or **through** this server, never "from" or "via". The
argument a call names a computer in is **computer**, and a reach stores the
computer's name as **ComputerName**.

## Decisions

**One table for both.** `agent_reach` holds, per agent, a kind (`skill` or
`server`), the name of the skill or server, and the computer's name. A skill
has no per-person row of its own to hang a column on, and one table keeps the
two alike in the API and the page. An empty computer is no row: the server.

**The setting is the default, and the agent may name another computer for a
call.** Every skill tool and every connected-server tool gains an optional
`computer` argument, taken off before the skill or server sees it unless the
skill declares one of its own. Leaving it out uses the setting.

**Choosing a computer that is not the setting asks first.** A request from the
person's machine comes from their network and their address, possibly inside
somebody else's. The setting is the person's decision; the agent choosing for
itself is not, so an explicit `computer` raises a skill call to at least a
write, which asks, the same as `web_fetch` through a computer. Connected-server
tools already ask unless the operator marked them read-only.

**A session per computer.** A connected server's session is stateful, so a call
through another computer uses a session of its own, cached under the server,
the person and the computer.

**A server that runs on "the computer" needs to know which one.** With one
computer attached it is that one. With several, it is the one set, and with
none set the call says so rather than picking one.

**The agent is told the settings.** The overlay listing the person's computers
says which services go through which, so the agent leaves `computer` out when
the setting is right.

## Progress

- [x] Migration, model and database operations for `agent_reach`.
- [x] Skills go through the computer the reach or the call's `computer` names.
- [x] Connected servers the same, with a session per computer.
- [x] API to read and set, and the card on the Connections tab.
- [x] Deployed and checked on a server with two computers attached.

## Surprises & Discoveries

A connected server set to run on the person's computer started on
`computers[0]`: the list is sorted by name, so with two attached it ran on
whichever name sorts first, which is an accident of spelling rather than a
choice anybody made.

Connected-server tools called over MCP panicked. `remoteRunner` found the agent
through `runOf`, which asserts the run is a conversation's `AskRun`; a call over
MCP runs in a `directRun`, and the assertion failed.

## Outcomes & Retrospective

Checked on a deployed server with two computers attached. With no reach, a
skill's requests went through this server; with its reach on the second
computer, all of them went through it, as its own log showed; a call naming
that computer did the same. A server that runs on the person's computer, with
two attached and no reach, kept its tools listed and answered a call with the
instruction to set its reach; with its reach set, it ran there.

Looking at it found one fault the tests had not: with two computers and no
reach, discovery refused the way a call does and the server's tools vanished,
so the person saw only a tool that no longer existed. Discovery now lists the
tools through any computer, since the list is the same on each.
