# Conversations

Where everything the agent has said and done is kept.

`internal/models/insight.go`, `internal/db/database_insight.go`,
`internal/agent/describe.go`, `internal/agent/tools/conversation`.

## Three kinds

- **main** — the one continuous conversation. There is exactly one per agent,
  made on demand. It is never deleted and never archived; it is compacted
  instead.
- **named** — a conversation kept apart, listed in the drawer's picker, which
  a person can rename, archive or delete.
- **run** — the transcript of a job nobody watched: a triage, a research pass,
  a scheduled run. Written so there is something to read afterwards.

A person can make a named conversation the main one, which demotes the old main
to named; the command line does it with `teanode agent conversation main`.

A run transcript can be talked into. Opening one from the agent page and asking
a question starts an ordinary turn in it.

## What a message is

One row per user turn, per round's answer, and per tool result, oldest first,
with the tool calls and a usage note on the answer. Two other roles appear:
`note` for what a turn did beside answering (stopped, failed, looked into
carefully, a goal set or met), and `compaction` for the note that stands in
for everything before it (`context.md`). Neither is sent to a model.

A note has a kind (`models.AgentNoteKind`) in its `name`, and only its
detail in `content`: the goal, the reason, the count. The drawer words each
kind in the person's language, and `models.NoteText` words it in English
for the command line and the chat channels. A run's `note` event carries the
same `noteKind` and `noteDetail` beside the English `note`, and a
compaction says `compacting` while it writes and `compacted`, with the note
as its detail, when it is done. A note without a kind is prose, shown as
written.

Reading a conversation returns the **newest** page — a drawer opens at the end
— along with the conversation's open todos.

## Titles and summaries

A background describer names conversations and writes the one line under them.

It looks once a minute, takes twenty conversations that have been quiet for
three minutes and whose last message is newer than their last description, and
asks a model for a title of five words or fewer and a one-sentence summary, in
the language the person writes in. It reads the last twelve user and assistant
messages, and at most the last six thousand characters of them: not the tools,
not the notes.

Three rules shape it:

- **A person's title wins for good.** Renaming a conversation marks it as
  theirs, and the describer never touches the title again. It keeps the summary
  current.
- **The main conversation is never titled**, only summarized. Every surface
  calls it the primary conversation.
- **A brand new named conversation is titled at once**, as soon as the first
  answer lands, rather than waiting for the quiet period — so the picker has a
  name for it.

The describer runs off the worker's tick in a goroutine, one at a time, because
twenty model calls would otherwise stop the tick claiming jobs.

## A goal

A **goal** is something the agent keeps at for the person between
conversations: "watch for the landlord's reply about the boiler and tell me if
none comes by Friday". Each runs in a conversation of its own, of the kind
`goal`, where the agent takes turns **on its own** that the person does not read
unless they open it, so the conversations they chat in stay theirs. Its turns
see the ones before them, which is the whole difference from a schedule, whose
run is a fresh transcript on a clock with no memory of the one before and no
way to say it is done.

The goal is columns on its conversation (migrations 0083, 0087 and 0148): a
title of a few words (`goal_title`), the description of what it is for and
what done looks like (`goal`), its state, the agent's one-line status
(`goal_note`), when the next turn is due, when it was set, and when it was last
said in the main conversation (`goal_surfaced_at`). The states are `working`
(turns are running), `waiting` (it needs the person), `met` and `dropped` (the
person stopped it).

Every turn of the agent's own ends with one call to the `goal` tool: `note`
with the status in one line and the minutes until the next turn, and
`activity` when something happened worth the person reading later; `wait` with
what it needs from the person, in a sentence they can answer; or `met`. The
tool also starts goals (`start`, with a title and what it is for), lists and
shows them, passes the person's words to one (`tell`), and lets the person
close or reopen one (`done`, `drop`, `reopen`). A goal's own turn starts no
goals and cannot tell, close or reopen one.

What happened on a goal is its **activity**, rows in `agent_goal_activity`:
started, progress (only when a turn said something happened; a turn that only
looked writes none), waiting, resumed, met, dropped, stalled and failed. What it
made is its **artifacts**: the schedules and background work made in its
conversation, found by that conversation, and the mail rules, reminders and
alert mutes its turns made, which the tools note in `agent_goal_artifact`. A
goal's turn may start background work; it wakes the goal's conversation, never
the main one.

The main conversation hears from a goal **only when it needs the person**: when
a turn says `wait`, or the goal stalls after twenty-four turns alone. The
worker's sweep (`surfaceGoals`) then writes one exchange there, as an alert is
written: a line opening with `models.GoalNeedsYouMarker` (`[goal needs you]`),
the goal's id and title, and the agent's sentence saying what it needs; it
publishes the events so an open drawer shows it, and the chat-app relay
forwards it. It writes once, recorded in `goal_surfaced_at`, and waits while a
turn runs in the main conversation. Progress, failures and meeting the goal are
not said there; they are in its activity. The mail a waiting goal sends is
kept.

The prompt of a turn in the main conversation lists the goals waiting on the
person, with their ids, so an answer given there is passed on with `tell`: the
person's words are written into the goal's conversation as a message opening
with `models.GoalRelayMarker`, with a `resumed` row, and the goal goes back to
work a minute later. Writing in the goal's own conversation resumes it as
before.

The check-in a turn arrives as opens with `models.GoalCheckInMarker`
(`[goal check-in]`), exactly, so a reader can tell it from the person's own
words, and says in the same breath that nobody is speaking.

There is one kind of goal. A conversation the person chats in carries none:
the drawer has no goal menu for it, and `StartAgentConversation` and
`UpdateAgentConversation` take no goal. Only goal conversations take turns of
their own; a goal left on another conversation from before is ignored.

How the turns are queued, bounded and delivered is in `jobs-and-schedules.md`.
The operations are `ListAgentGoals`, `GetAgentGoal`, `StartAgentGoal`,
`TellAgentGoal` and `SetAgentGoalState`; `teanode agent goal` and the Goals tab
of the agent's page use them. The design is
`docs/planning/background-goals-execplan.md`.

## Searching

Searching matches the title, the summary, and what was said in user and
assistant messages, newest first, archived ones included. It does not reach run
transcripts, and it does not read tool results.

The `conversation` tool gives the agent the same three things: `search` over
main and named conversations, `list` over every kind including run transcripts,
and `read` for one conversation from its end, with a note saying how many
earlier messages there are.

Two rules hold in `read`. Another agent's conversation reads exactly like one
that does not exist, and nothing is loaded before that is checked. And a secret
a tool showed once — an API token, an app password — is replaced by a note
saying one was there: it was to be relayed once and kept nowhere, so it is not
read back into another conversation.

## Retention

Once an hour the worker sweeps, using the two retention settings:

- `agent.retention.runs`, thirty days by default, removes **run transcripts**
  and their messages, every **finished job** row, and the agent's **reply
  records**.
- `agent.retention.corrections`, ninety days by default, removes recorded
  corrections.
- Separately, uploaded files nobody claimed within a day are removed with their
  bytes.

Main and named conversations are never swept. They grow until compaction folds
their beginning into a note.

## Caveats

- **A person's title is theirs; their summary is not.** The describer keeps
  rewriting the summary of a conversation the person named.
- **A title given at creation is not marked as the person's**, so the describer
  will replace it. Renaming afterwards is what makes it stick.
- **A search query is not escaped**, so `%` and `_` in what a person types act
  as wildcards.
- **Archived conversations are still readable by the tool**; it does not check.
- **A goal is only as bounded as the budget, the floor and the day's cap.**
  Nothing reads what the goal says before the turns start, so a goal that
  cannot ever be met runs its forty-eight turns a day until somebody clears
  it.
- **Files attached to a run transcript outlive it.** The sweep removes the
  transcript and its messages, but attachments are not tied to a conversation
  by a foreign key and only unclaimed ones are collected, so artifacts made
  inside a run leave their rows and their bytes behind.
