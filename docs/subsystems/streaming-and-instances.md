# Streaming, and more than one instance

How a turn is watched while it runs, and what happens when the server is more
than one process.

`internal/agent/ask.go` (events), `feed.go` (the conversation feed),
`command.go` (words back to a run), `internal/api/v1api/apigraph/agent_ask.go`
and `websocket.go`, `web/src/api.ts`, `internal/channel`, `internal/cmd`; for
chats `internal/chat` (the hub).

## Events

A run emits a stream of events. Every one carries the run, the conversation, a
per-run sequence and a time.

| Kind | When |
| --- | --- |
| `asked` | first, before anything else: what was said, and the surface it came from |
| `text` | a piece of the answer, as it streams. In a chat the pieces of an agent's answer are of no run's feed: they go to every member's conversation through the chat hub, with `runId`, `authorAgentId` and `name`, at most four times a second, and a client draws them as a provisional message that the stored `said` (its `message.agentRunId` is the run) replaces |
| `message` | the whole answer of a round, once it is stored |
| `tool_call` | the model asked for a tool |
| `tool_result` | the tool answered: the bounded, wrapped content, plus a line for the drawer |
| `confirmation` | a card is waiting: tool, arguments, risk, preview |
| `question` | `ask_user` is waiting: the question and any choices |
| `note` | queued, stopped, compacted, titled, out of rounds |
| `navigate` | `open_page` asks the dashboard to show a page of itself: the path |
| `locate` | the `location` tool asks the browser the person is chatting in where it is; the drawer that sent the turn, named by the `drawerId` it sent with `AskAgent`, answers with `AnswerAgentLocation`; for a turn no drawer sent, the one the person sent from last, while it is in view |
| `reacted` | a reaction on a message changed: `messageId`, `emoji` (empty when it was taken away) and `reactorKind`, `person` or `agent`; in a chat also `reactorUserId` and `reactorName`. Of no run, like a new title: no `runId`, never replayed |
| `said` | a chat conversation only: a new message by a person or an agent, whole in `message` (an `AgentMessage` as this member reads it, so `isMine` is theirs); `isMessageCut` says it was too long to travel to the other instances, cut or left out, and the client reads the conversation again |
| `edited` | a chat conversation only: a message was changed; `message` is the new one |
| `unsaid` | a chat conversation only: a message was taken back; `messageId` |
| `read` | a chat conversation only: a member's read mark moved to `messageId`; `userId` is whose |
| `typing` | a chat conversation only: `userId` and `name` of the member who is writing, or, while an agent's turn in the chat runs, `authorAgentId`, `name` and `authorOwnerName` of the agent that is thinking (the `text` events of its answer carry the same), sent when the turn starts and every four seconds to every member but the agent's owner; nothing is stored |
| `members` | a chat conversation only: a member changed what their agent does in the chat (remembers it, may be asked by the others); read the chat's members again |
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

**A chat.** A chat conversation's feed carries the events of the chat, which
belong to no run and are never replayed (the table above has them: `said`,
`edited`, `unsaid`, `read`, `typing`, and `reacted` with the reactor's name).
They do not travel through the agent. A server with no language model builds no
agent, and two people in a circle can still write to each other there, so the
feed of a chat is the chat hub's (`internal/chat`, `Hub`), made at start
whatever the agent configuration is and handed to the GraphQL settings.
`AgentConversationEvents` on a chat conversation subscribes to the hub
(`Hub.Subscribe`), after the same membership checks as for any conversation,
with or without an agent worker; the worker's feed of that conversation is
followed beside it when there is one, for the agent's own turns in a chat. For
any other conversation the worker is still the only feed, and a server without
one answers "agent: unavailable".

What the people say is stored once, in the chat, and published once for each
member who is in it now, to that member's own conversation, with the message
as that member reads it (`Hub.PublishChatEvents`, with the events built by
`chat.EventsForMembers`, which leaves out a member who was taken out, so they
stop being told at once; `internal/api/v1api/apigraph/chat.go`
`publishChatEvents`). Saying, typing, reading, reacting, editing and taking
back all go through it. When an agent takes part in a chat, its posted answers
and its typing go out through the same method, which `agent.Agent` reaches as
`PublishChatEvents` through `agent.Settings.ChatEvents`. Nothing about a chat
is in the agent's own relay.

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
relays what its own runs emit and applies what it hears from others. The
events of chats travel the same way on a channel of their own, `chat_event`
(`db.ChatEventChannel`), relayed and heard by the chat hub, so that a server
that runs no agent has them too. Both carry the instance's name with each
event, so an instance does not hear itself, and both trim a payload alike
(`agent.FitEventForRelay`); a chat message too long to travel whole is cut
with `isMessageCut` set, or left out, and the client reads the conversation
again.

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

- **Who is looking is local.** The dashboard and the app report each minute
  whether they are shown (`ReportAgentPresence`), to the instance their
  request reached, into one `agent.Presence` shared by the chat hub and the
  agent. A chat message is sent to a phone only when its person is not looking
  as that instance knows it, so behind a load balancer a person may be sent
  what they were reading on another instance.
- **A run is local.** Reading a run's events, or polling it, only works on the
  instance running it. Only stop, answer, resolve and locate have a way across.
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
