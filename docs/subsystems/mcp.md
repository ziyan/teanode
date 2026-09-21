# The Model Context Protocol, both ways

The Model Context Protocol is the convention programs use to tell each other
"here are the things I can do, call them by name". This server is on both ends
of it. It **connects out** to servers a person has, so their tools become the
agent's tools. It also **answers** the protocol itself, so a program the
person runs — a coding harness, an editor — can use the agent's tools, and can
put a question to the agent in words.

Keep the two directions apart while reading. They share a wire format and
almost nothing else.

| | Outward | Inward |
| --- | --- | --- |
| What it is | a client calling somebody else's server | a server a harness calls |
| Where | `internal/mcp`, `internal/agent/tools_mcp.go` | `internal/mcpserve`, `internal/api/v1api/apigraph/agent_mcp.go` |
| Who declares it | the operator declares a server; a person connects to it | nothing to declare; an API token is the whole of it |
| What it adds | the outside server's tools, namespaced, in the person's catalog | the person's whole catalog, to the harness |

## The wire format, in brief

Messages are JSON-RPC 2.0: an object with `jsonrpc`, `method`, `params` and an
`id`, answered by an object carrying the same `id` and either `result` or
`error`. A message with no `id` is a *notification* and gets no answer at all
— answering one puts a message on the wire the other end is not waiting for.

A session opens with `initialize`, followed by the `notifications/initialized`
notification. After that the only two methods that matter here are
`tools/list` and `tools/call`. Prompts, resources and sampling are parts of
the protocol this server neither offers nor consumes.

A tool that fails is carried as a *successful* response whose result has
`isError` set, not as a JSON-RPC error. The distinction is the whole
difference between "the tool said no" — which a model on the other end can
read and work around — and "the call never happened", which tells it the
server is broken.

## Outward: servers the agent connects to

An operator declares a server in the configuration or through the
`connected_server` tool: a name, a URL, and how people authenticate with it
(nothing, a shared credential, the person's own, or OAuth). A person then
connects their own credential, or authorizes in a browser for a server using
OAuth 2.1 with PKCE. Their credentials are sealed with the server secret
before they are stored.

`internal/agent/tools_mcp.go` discovers each connected server's tools and
holds the list for five minutes; three failures in a row withdraw a server's
tools until the next interval, so one service being down does not take the
agent's whole kit with it. What comes back is namespaced by server name and
joined into the person's catalog, and is **outward** risk — needing the
person's word — unless the operator listed a tool as read-only.

Everything a connected server returns is data. It never instructs.

## Inward: a harness using the person's tools

`POST /api/v1/mcp`, authenticated by an ordinary API token. There is no
session: every request carries the token, and nothing is kept between
requests, so a restart loses nothing.

`internal/mcpserve` is the protocol and nothing else — no HTTP, no database,
no idea what a tool is. It takes one decoded message and returns the one to
send back. The file in `apigraph` is the transport and the caller: who is
asking, what they may reach, and turning one POST into one answer. It lives in
`apigraph` rather than in a component of its own because resolving the caller
and building the API-as-the-person are both already there.

Two ways to connect:

    # anywhere, with a token
    claude mcp add --transport http teanode \
        https://mail.example.com:10443/api/v1/mcp \
        --header "Authorization: Bearer tnt_..."

    # on the same machine as the command line, with no token to paste
    claude mcp add teanode -- teanode agent mcp serve

The second is a pipe, not a second implementation: `teanode agent mcp serve`
posts each message to the same endpoint with the profile's own credential and
writes the answer back, so whatever the server offers it offers.

### What is offered

The person's whole catalog, filtered the way a conversation's is: what their
permissions allow, less what the operator switched off, plus the tools of the
servers they have connected and the skills the operator installed. Somebody
who may not read mail is not offered `mail_search`, because the tool is not in
their catalog to begin with.

This grants nothing a token did not already grant. An API token carries its
account's whole permissions and the GraphQL API beside this endpoint already
exposes every operation those permissions allow. The endpoint changes the
shape of the request, not what may be asked. The boundary that matters is the
token, and it is the operator's to hand out.

Two tools are left out, and both for the same reason — there is nobody at the
harness end that this server can reach.

`ask_user` exists to put a question in front of a person who is watching. A
call arriving over the protocol has a person at the other end, but they are
looking at their harness, and this server cannot draw on it. A tool that
reaches for `Run.Ask` gets an error saying so rather than a guessed answer.

`tool_search` exists to keep a long catalog out of a model's request until it
is wanted. A harness lists tools once and keeps the list, so it is better
served by the list.

### Running a tool

A `tools/call` runs the tool. It does not start a conversation in which a
model decides to run it: the caller already knew what it wanted, and a model
in the middle would cost money, take seconds and answer differently each time.

What a tool needs is the run it is called in — who it is for, what it may
reach, where its work is filed — and in this codebase that is an interface
(`tools.Run`) rather than the conversation loop. `internal/agent/direct.go`
implements it for a caller that is not a conversation: the token's person as
owner, the same `agentOperations` the loop uses so every call is executed and
audited as them, no conversation, a surface of `mcp`.

Calls arrive marked confirmed. A harness asks its own person before it runs a
tool, and there is no card this server could show that would reach them; the
credential they handed the harness is what says they meant it.

Anything a tool marks untrusted is wrapped before it goes back, the way the
conversation loop wraps it. The harness hands it to a model of its own, which
needs telling as much as ours does.

### Asking the agent

`teanode_ask` is the one tool here that is not in the agent's own catalog. It
takes a question in words and returns the agent's answer, with the whole of
its kit and its memory of the person behind it. A harness that would otherwise
orchestrate six calls asks once.

It is bounded by what is at the other end rather than by what a turn takes. A
harness gives a tool call about a minute before it gives up, so waiting for a
long turn would throw the work away at the moment it was about to pay off.
After 55 seconds it returns whatever has been said and names the conversation;
the turn carries on, and calling again with that conversation and no question
collects the rest. A turn that finished in the meantime is handed back too and
replays what it said, so coming back late still hears the answer. A run is
forgotten ten minutes after it ends, and after that the conversation itself is
where the answer is.

A turn that stops to raise a confirmation card cannot be answered from a
harness. The answer says so, and says the card is waiting in the dashboard,
rather than looking like an answer that simply stopped.

### Connecting this server to itself

Nothing stops an operator declaring this endpoint as one of the agent's own
connected servers. The agent would discover a namespaced copy of its own
catalog and a call into it would come straight back out. The handshake refuses
a client that says it is TeaNode, which is where our own client says so. And
`teanode_ask` is never in the agent's catalog, so even a loop past the first
guard cannot recurse through it.
