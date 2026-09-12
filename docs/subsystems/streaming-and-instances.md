# Streaming, and more than one instance

How a turn is watched while it runs, and what happens when the server is more
than one process.

`internal/agent/ask.go` (events), `feed.go` (the conversation feed),
`command.go` (words back to a run), `internal/api/v1api/apigraph/agent_ask.go`
and `websocket.go`, `web/src/api.ts`, `internal/channel`, `internal/cmd`.

## Events

A run emits a stream of events. Every one carries the run, the conversation, a
per-run sequence and a time.

| Kind | When |
| --- | --- |
| `asked` | first, before anything else: what was said, and the surface it came from |
| `text` | a piece of the answer, as it streams |
| `message` | the whole answer of a round, once it is stored |
| `tool_call` | the model asked for a tool |
| `tool_result` | the tool answered: the bounded, wrapped content, plus a line for the drawer |
| `confirmation` | a card is waiting: tool, arguments, risk, preview |
| `question` | `ask_user` is waiting: the question and any choices |
| `note` | queued, stopped, compacted, titled, out of rounds |
| `error` | the turn failed |
| `done` | always last |

`text` and `message` overlap by design: the deltas stream, then the whole
answer arrives once it is on disk. A consumer replaces rather than appends.

Two bounds matter. A run keeps the **first** 500 events for replay, not the
last, so a late subscriber to a long turn gets its beginning and then follows
live. And a subscriber that stops reading loses events rather than stalling the
run.

## Two ways to watch

**A run.** `AgentRunEvents(runId)` and the polling `ReadAgentRun` follow one
run. The command line uses the poll: it asks for everything after a sequence,
waits up to twenty-five seconds, and returns as soon as anything arrives.

**A conversation.** `AgentConversationEvents(conversationId)` follows every
turn of a conversation, whoever started it and wherever it runs. This is what
the drawer uses, which is why a turn typed into Telegram appears in the browser
as it happens. On subscribing, the unfinished runs of that conversation on this
instance are replayed; after that it is live. The subscription never completes
on its own.

Replay and live delivery can hand over the same event twice. The resolver keeps
the highest sequence seen per run and drops anything not newer.

## The websocket

One small protocol on the same path as the GraphQL endpoint. The client sends
`connection_init` carrying either a bearer token or the CSRF token that matches
its cookie, and gets `connection_ack`. Then `start` with the document, `data`
per result, `stop` to end one, `complete` when a subscription finishes,
`error` when the document itself was refused. The server sends `ka` every
second.

The dashboard's client treats those two errors as final — a refused document
will not be fixed by another socket — and everything else as worth
reconnecting for: it backs off from one second to thirty with jitter,
reconnects at once when the network or the tab comes back, and replaces a
socket that has said nothing for fifteen seconds. Before every start,
including the first, it reads the transcript again, so the replay lands on a
fresh picture.

Cards, questions, the queued note and errors live only in the stream. A
transcript read draws what was stored, so those lines disappear when the drawer
re-reads.

## What crosses instances

Events travel as PostgreSQL notifications on `agent_event`. Each instance
relays what its own runs emit and applies what it hears from others.

- **Pieces of an answer are joined** before they travel, so a fast model is not
  one notification per word. Sequences on a foreign run are therefore sparse.
- **A payload is trimmed to fit.** The limit is just under eight kilobytes, so
  the text, then the arguments, then an error, then the note are shortened in
  that order until it fits, on a character boundary and with a mark. An
  oversized payload would be refused whole, which would lose the event.
- **Nothing is kept.** A listener that reconnects has lost what was said while
  it was away; the transcript is the record.

Words back to a run — stop, answer a question, decide a card — travel on
`agent_command`. The instance that holds the run applies them. Ownership is
checked before anything is sent: the run is either here, where its conversation
is checked directly, or it is one this instance has heard of through the feed
within the last hour, whose conversation is then looked up and checked against
the caller's own agent.

The job queue claims rows with `FOR UPDATE SKIP LOCKED`, and a claim that goes
stale after fifteen minutes returns to the queue. A chat app's bot is claimed
the same way on its own row, renewed every thirty seconds for ninety.

## What does not cross instances

This is the important list.

- **A run is local.** Reading a run's events, or polling it, only works on the
  instance running it. Only stop, answer and resolve have a way across.
- **The one-turn-at-a-time rule is local.** Two instances can run two turns of
  one conversation at once.
- **Attached tabs and attached computers are local**, held in memory by the
  instance holding the websocket. A turn that lands elsewhere cannot see them,
  and their tools are left out of the round.
- **Connected server sessions and headless browser contexts are local**, and
  the browser context cap is counted per instance.
- **Concurrency is per instance**, as the configuration says.
- **The describer takes no claim**, so two instances can title the same
  conversation and pay for both calls.
- **Stopping every run of a conversation is local**, which is what deleting a
  conversation and a chat app's `/stop` both use.

A deployment behind a load balancer wants session affinity for the dashboard
and the command line, or turns will be started on one instance and polled on
another.

## The surfaces

**The drawer** subscribes to the conversation while it is open, and redraws a
turn from its `asked` event on, so a turn that started elsewhere appears
complete. Its own in-flight send is recognised and not drawn twice.

**A chat app** subscribes to the run it started. It types while it works,
streams the answer into one message it keeps editing, deletes that message when
a tool call interrupts, sends cards as questions to reply to, and sends what the
agent made when the turn is done.

**The command line** never opens a websocket. It polls the run, prints whole
answers rather than streamed text, and answers cards on the terminal.
