# Conversations

Where everything the agent has said and done is kept.

`internal/models/insight.go`, `internal/db/database_insight.go`,
`internal/agent/describe.go`, `internal/agent/tools/conversation`; for chats
`internal/models/chat.go`, `internal/db/database_chat.go`,
`internal/api/v1api/apigraph/chat.go`, `internal/chat` (the hub that carries a
chat to its members), `internal/agent/chat.go`.

## Five kinds

- **main**: the one continuous conversation. There is exactly one per agent,
  made on demand. It is never deleted and never archived; it is compacted
  instead.
- **named**: a conversation kept apart, listed in the drawer's picker, which
  a person can rename, archive or delete.
- **run**: the transcript of a job nobody watched: a triage, a research pass,
  a scheduled run. Written so there is something to read afterwards.
- **goal**: the conversation of one goal, where the agent takes its turns
  toward it out of the person's sight; see A goal below.
- **chat**: a member's own conversation for a chat with other people in it;
  see Chats below. A member can archive it, with the agent on or off, and it
  leaves only their own list: `UpdateAgentConversation` sets `archived_at` on
  their row, the listing follows it as for a named conversation, and a new
  message does not bring it back. An archived chat is as quiet as a muted one:
  no phone notification for it (`NotifyChatMessage` skips a member whose
  conversation is archived), and it stays out of the unread count, which is
  read from the live list.

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

## Reactions

The person and the agent can each put one reaction on a message somebody
said, as in a chat app: one or two emoji, replaced by reacting again and
taken away by reacting with nothing (`agent_message_reaction`, migration
0162). The person reacts from the drawer's hover actions, with
`ReactToAgentMessage`, or with `teanode agent react`; the agent with the
`react` action of the `conversation` tool, on the person's latest message
unless it names another (`message_id`, a message of the person's own conversations), without a card. A message of a
chat is named with `chat_message_id` instead, and that is outward, because everybody in the chat sees the reaction
under the agent's name, so it asks first; a chat message given as a `message_id` is turned back, saying to use
`chat_message_id`. Every change is a `reacted` event
on the conversation's feed (`streaming-and-instances.md`).

The agent does not leave the first reaction to its model, which often
skipped it. When a turn begins with the person's own message in a main,
named or goal conversation, on a surface that draws reactions (`hasReactions`
in `surface.go`: the drawer, the extension and the app, not a call, Siri, the
terminal or a run), `acknowledge.go` makes one small call with no tools on the
compact or ask model, beside the turn. It picks one of eight emoji, or none,
and stores it unless the agent has reacted already. The `react` action
replaces it, and is refused in a run's transcript, where nobody sees it.

A reaction starts no turn. Messages are read with their reactions, and the
history the model is sent says each one after its message: "(The person
reacted 👍)", or "(You reacted 🙏)" for the agent's own. Why is in
`docs/decisions/20261011-a-reaction-is-a-mark-on-a-message-and-starts-no-turn.md`.

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

Work on a clock belongs in a **schedule the goal owns**: one made in the goal's
conversation, given to `start` as `schedule_ids`, or moved to it with the
tool's `take_schedule` (`MoveAgentScheduleToGoal`, `teanode agent goal
take-schedule`). Its runs take place in the goal's conversation and are the
goal's turns: each ends with the goal tool, and only a `wait` reaches the
person. While a goal has an enabled schedule it takes no turns of its own; when
the schedule is switched off or removed, the worker gives the goal a turn
again. A goal remembers the conversation it was asked for in, which its page
links to (migration 0149).

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

## Chats

A **chat** is a conversation with people in it, `direct` between two people or
`group`; every circle also has one **circle chat** whose members are the
circle's (`circles.md`). It is listed, read, followed, written to and reacted
to with the same operations as a conversation with the agent
(`ListAgentConversations`, `ReadAgentConversation`, `AgentConversationEvents`,
`AskAgent`, `ReactToAgentMessage`); there is no separate page.
`docs/decisions/20261016-a-chat-is-a-conversation-with-people-in-it.md` says
why.

**How it is stored.** What people and agents say is stored once, in
`chat_message` (migration 0194). `chat` is the room, `chat_member` who is or was
in it (with the read mark, the mute, whether the member's agent remembers it and
whether the others may ask that agent there), `chat_reaction` one reaction for
each person on a message. Each member also has their own `agent_conversation`
of the kind `chat`, with `chat_id` pointing at the room, made in the
transaction that adds them. It holds its place in the person's list and the rows
the member's own agent writes in a chat (its tool calls, their results, its
notes and cards), which is how only that member reads them.
Everybody who can chat has an `agent` row, made switched off
(`EnsureAgent`) for a person who never turned theirs on, because their
conversations and phones hang from it. Nothing treats that row as "this person
uses the agent": the sweeps over every agent (the dream, speaking first,
follow-ups, the weekly look around, evaluation) check `Enabled` or `Active`,
which it is not, and the places that ask whether the person has an agent at all
(their settings page, the operator's list of agents, granting a mailbox or a
source, learning from a correction) see a **placeholder** as none
(`Agent.IsPlaceholder`: switched off and not changed since it was made). The
first time the person saves anything about their agent the row is theirs. A
row is made when a person joins a circle or is added to a chat, or lists their
conversations while in a circle, never for a person in no circle.

**Who reads what.** Reading a chat conversation returns the chat's messages
from the time the member joined, until the time they left if they did, together
with that member's own `agent_message` rows in the conversation, by time. A
message is an ordinary `AgentMessage` with the role `user` when a person wrote
it and `assistant` when an agent did, and with an author (`authorKind`,
`authorUserId`, `authorName`, `isMine`, ...) that every conversation's messages
carry, so a client draws all of them one way. An agent's message also carries
`authorOwnerName`, its owner's name as the chat knew them (kept in
`chat_message.author_owner_name`, so it outlives the owner's account), because
most agents keep the name "Agent" and two people's agents in one chat would
otherwise be indistinguishable. Every surface names an agent the same way:
"<agent name> · <owner>'s agent", or just "<owner>'s agent" while the agent
still has the default name (`models.AgentSpeakerLabel` for the command line and
phone notification titles, `agentSpeakerName` in the dashboard,
`AgentSpeaker.name` in the app). Nobody reads another member's
conversation, an operator included: not to read it, ask in it, or react in it as
them. A deleted message keeps its row and loses its words and files; an edited
one has `editedAt`. A reply shows the words of the message it is to, except to a
member who joined after that was said: the reply still says which message it is
to, and neither a read nor the `said` event told to that member carries its
words (`quotedChatMessages`, `withoutQuoteBeforeJoining`), and a member cannot
reply to a message from before they joined. A member who left or was
taken out keeps reading what was said while they were there and can write
nothing more ("you are no longer in this chat"). Coming back starts their
reading afresh.

**Who is in it.** A direct chat is unique for the two people
(`direct_key`), so opening it twice finds the same chat, and needs the two to
share a circle; once they no longer do it stays readable by both and refuses
messages ("you no longer share a circle"). A group chat's members must each
share a circle with whoever adds them, its maker is its first admin, and only
its admins rename it, add and remove people and delete others' messages. A
group chat holds at most 50 people at once, and one person starts at most 20 in
a day (those still there are counted); the refusal says which. People in a chat
together see each other's pictures for as long as they are both in it, whatever
circles they share (`canSeeAvatar`), since the chat already shows their names. A
circle chat is changed only through its circle, in the same transaction: a
person who joins is added, one who leaves is taken out, and so is a person
from every group chat in which the leaving left them sharing a circle with
nobody. A circle that predates chats gets its chat the first time one of its
members lists their chats (`SyncChatsOf`). When an account is deleted its
membership ends and its words keep the name it had; the screens show it with
"(deleted account)".

**Writing.** `AskAgent` on a chat conversation stores a chat message, publishes
it and notifies phones. It starts a turn only of an agent it addresses (below),
and answers the run of the caller's own agent in that case, an empty run id
otherwise. The
caller's uploads (`agent_attachment`) are claimed by the message and read by
the other members through the attachment URL. A file sent by a person whose
agent is off is kept, but the text read out of it for a model is not, because
there is no model to spend it on. A person may send 60 messages a
minute on one instance (`ratelimit`), a message is at most 16,000 characters,
and it needs words or a file. A person keeps at most 1GB of the files they sent
in chats (`agent.limits.maxChatAttachmentAccountBytes`, `db.ChatAttachmentBytes`):
a message with files stays until somebody deletes it, and nothing sweeps
them, so a send past the limit is refused with how much they keep and that
deleting old messages with files makes room. An edit, a reaction and a deleted
message are told only to the members the message was readable by
(`publishChatMessageEvents`), so somebody added to a group later is never handed
the new words of what was said before them. `SayInChatAsAgent`, which the `conversation`
tool's `send` action calls, writes the same message as the agent: its name, and
`author_agent_id` set. Unread counts come from the member's read mark, which
only moves forward (`MarkChatRead`) and which writing a message also moves.
"Forward" and "newer" are by when a message was stored and then its identifier,
the order a chat is read in, for the read mark, the unread counts and the
cursor a chat source reads from: identifiers made by different server instances
are not in the order of their clocks, so comparing the identifiers alone could
skip or repeat a message.

**Told.** Who is in a chat is told as well: adding, taking out, leaving, a
circle's change to its chat and a circle ending send a `members` event to the
conversation of everybody in the chat now and of those the change took out, once
it commits, and a screen that has the chat open reads its members again. The
events are in `streaming-and-instances.md`: a chat's events and
its phone notifications belong to the chat hub (`internal/chat`), which a server
builds whether or not it has a language model, so chats are live, typing and
read marks show, and phones are told on a server that runs no agent. A phone is
told of a message (the notification kind `chat`, titled with the author's name
and, in a group, the chat's) when its person is not the author, has not muted
the chat and is not looking at that chat in a dashboard or the app
(`Hub.NotifyChatMessage`): a shown tab or the app on another screen, or on
another conversation, does not hold a message back; the phone's thread is the
member's own conversation. The phones hang from the
person's `agent` row, switched off or not, and the operations that register,
list and remove them need `circle:use` as well as `agent:use` for that reason.
Who is looking is the `agent.Presence` the hub and the agent share, which the
dashboard and the app report to even with the agent off. Each report names its
reporter and the conversation it shows (`conversationId`, empty when it shows
none: the drawer closed, or the app on another screen), and
`Presence.IsWatchingConversation` is true while a shown, recently used reporter
has that conversation on screen.

**Agents in a chat.** A message starts a turn of an agent only when its author
addresses that agent: the composer's `@` chooses it, or replying to one of its
messages, and the client sends the agent's id as `addressedAgentIds` (the
command line's `--ask-agent` addresses the sender's own, without knowing its
id: `shouldAddressOwnAgent`, and `--address` names others by agent id or by
their owner's username). The server adds the agent of a message that is
replied to by itself (`repliedAgentOf` in `chat.go`), so a client that only
sends the reply reference, as `--reply-to` does, addresses the same agent the
dashboard and the app name. An agent's name in ordinary prose addresses
nothing. `chat.DecideAgentTurns` (`internal/chat/agents.go`, a function of the
author, the ids and each member's agent, with no database) is the whole rule:

- A message an agent wrote starts nothing, of any agent, and neither does one
  by somebody who is not in the chat. So two agents in a room cannot talk to
  each other.
- The author's own agent, addressed, starts an **owner turn**. If it is
  switched off, nothing starts and the author gets a note that it is off.
- Another member's agent, addressed, starts a **member turn** only if its owner
  turned on "others may ask my agent here" for this chat (`chat_member.
  is_agent_askable_by_others`, `SetChatAgentAskableByOthers`) and the agent is
  on, which also needs its owner's account to be enabled and to hold agent:use,
  and the author to hold agent:use themselves. If the owner did not allow it, or
  the author may not use agents, nothing starts and the author gets a note that
  only its owner can ask it; if the owner did but the agent is off, a note that
  it is off. An id that is nobody's in this chat gets a note that it is not here. At
  most four agents are started by one message.

The notes are rows of the author's own conversation (`chat_agent_owner_only`,
`chat_agent_off`, `chat_agent_absent`, with the agent's name as the detail) and
a `note` event to that conversation alone: nobody else is told that somebody
asked, and nobody learns whether an agent they may not ask is on.

Both turns are `Agent.Ask` in the owner's chat conversation with the surface
`chat` (`internal/agent/surface.go`: no suggested replies, no page to move the
dashboard to, no location asked of a browser, reactions drawn) and an
`AskSettings.Chat` (`internal/agent/chat_turn.go`). The turn is started once the
message has committed, because it reads the message from the database; its run
id is chosen first (`AskSettings.RunID`) so the request can answer it. The model
reads the conversation as its owner reads it: `chat_message` rows interleaved
with the owner's own agent rows (`ListChatTurnRows`; the newest 400 until a
compaction folds the older part into a note, and then everything from the message
the note stands in for). What the owner wrote is a user turn, what their agent
said is an assistant turn, and what anybody else or any other agent said is a
user turn under their name, one line and at most 60 characters, with their words
inside `<untrusted-data>` (`fenced`: the closing tag in a message cannot end it).
A round's words and its calls were stored apart (the words as the chat's message,
the calls as the owner's row); they are put back as the one assistant turn they
were. The message that started the turn comes last, so a message written while
the turn waited for the one before it is read before it. A second message that
addresses the agent while a turn runs queues a turn behind it; a chat turn is
never steered. The conduct has a short section, rendered only on this surface,
that names who is in the room and which one is the person the agent acts for,
says everybody reads the answer, and not to bring in anything private the
question did not need.

*What a turn posts.* The words of each round are stored as a `chat_message`
(`author_agent_id` the agent, `author_user_id` its owner, `author_member_name`
the agent's name, `agent_run_id` the run) and told to every member as `said`;
phones are told too, the owner's included, whose answer it is. Its calls, their
results, its notes and its cards stay rows of the owner's conversation: a round
that called a tool leaves an assistant row with the calls and no words. No tool
result can reach a chat message, because the only text posted is the model's own
answer. The final round of a turn, which has no calls to leave a row behind, leaves an
assistant row of the owner's own with no words and the round's cost (and the
cost of the judgments carried to it), so that what a chat turn cost is summed
from the owner's rows like any turn's and never from the chat's message, which
everybody reads; the history leaves such a row out. The agent's message does not move its owner's read mark, so the answer
counts as unread for an owner who was not looking. While the turn runs every
member but the owner gets a `typing` event carrying the agent's name,
`authorAgentId` and `authorOwnerName` when it starts and every four seconds until it ends, which the
clients word "is thinking" (a person is "is typing"); and the words as they
stream are told to every member, the owner included, as `text` events with the
run and the agent, at most four times a second, which a client draws as a
provisional message that the stored `said` replaces. The run's own feed carries
the same turn to the owner (`asked`, `tool_call`, `tool_result`, `confirmation`,
`message`, `done`) but no `text`: in a chat the words come through the chat. A
card a turn raises is a row of the owner's conversation, so only the owner sees
it.

*The member turn.* It is limited to the chat and the web. It runs on
the owner's agent, budget and usage (the owner's usage page shows it), with the
settings `applyChatSettings` imposes and a caller cannot loosen: read only, the
tools `web_search`, `web_fetch`, `datetime`, `conversation` and `tool_search`
and no other (no mail, memory, files, calendar, finance, devices, skills or
connected servers; and nothing the owner shared with particular people, a
shared calendar or shared pages, because the answer goes to the whole room and
the owner shared those with them and not with it), at most eight rounds and six searches and six fetches, and
nobody to ask, so a call that would need a card is refused and never shown to the
owner. At most three turns of an agent run or wait in a chat; a question from
somebody else past that is refused (its asker is told it failed) and the
owner's own never is. The prompt carries nothing of the owner's: no index of
what the agent knows, no recall, no mailboxes, goals or permissions, none of
their instructions. Nor does the history: it is the chat's messages alone,
never the rows the owner's agent wrote in the conversation (the results of its
tools hold what it found in their mail, memory or files) nor the compaction
notes made from them, and it is never compacted. It starts at the latest time
any current member joined: the answer goes to everybody in the room, and what
was said before one of them came is not theirs to be told of, whoever asks (the
owner's own turn keeps the owner's window, since the owner chose to speak in
front of the room). The turn thinks at the default effort whatever the asker
named, since the budget is the owner's. The `conversation` tool reads only this
chat in such a turn, from the same time, and only the people's messages, not
what the owner's agent did in it. The asker's
words are inside the fence.

*Remembering.* `SetChatAgentReading` makes the chat a knowledge source (kind
`chat`, `specification.chatId`) of the caller's agent, from the time it was set:
the source's cursor starts after the newest message there is. A pass files what
was said since, a document for each day's run of messages, marked private (a
chat is for its people); the dream reads it like any source. Turning it off, or
leaving the chat, switches the source off at once and bumps its generation so a
pass in flight stops filing; what was learned stays, as with any source, and
turning it on again reads from then. The other members see `isAgentReading` on
every member. A `chat` source is made only by that switch.

**What does not touch a chat conversation.** The background describer (titles
and summaries), the remember run that files what a conversation taught, search
of conversations by their words, the acknowledging reaction, goals and
background wakes all work on the main, named, goal and run kinds only; a chat
conversation is none of them. Compaction does work on one: the note goes in the
owner's agent rows and never into the chat. `UpdateAgentConversation`,
`DeleteAgentConversation` and `SetAgentMainConversation` refuse a chat (use
`RenameChat` and `LeaveChat`).

On the command line `teanode agent conversation list --kind chat` lists them,
`show` prints who wrote each message and who has left, `members` who is in it,
how far each has read and what their agent does there, and `teanode agent ask
--conversation ID` and `teanode agent chat --conversation ID` write in one;
`--ask-agent` on `ask` asks your own agent there and follows the turn like an
ordinary ask, `--address ID-or-username` (repeatable) asks another member's
agent that its owner let the chat ask, and a line of `agent chat` that starts
with `@agent` asks your own.
`direct`, `group`, `add`, `remove`, `leave`, `mute`, `read`, `edit` and
`delete-message` do the rest, and `remember ID on|off` and
`others-may-ask ID on|off` are the two switches. `agent chat` finds what the others say by looking every couple of
seconds, as a terminal cannot be pushed to.

## Searching

Searching matches the title, the summary, and what was said in user and
assistant messages, newest first, archived ones included. It does not reach run
transcripts, and it does not read tool results.

The `conversation` tool gives the agent the same three things: `search` over
main and named conversations by what was said, and over the person's chats by
their title and the names of the people in them (as the dashboard's search
does; what was said in a chat is not searched), `list` over every kind
including run transcripts and the person's chats, and `read` for one conversation from its end, with a
note saying how many earlier messages there are, each with its id and
reactions; in a chat each message says who wrote it, and the answer is marked as
other people's words. Its fourth action, `react`, is the agent's reaction
(above), on a message of a chat too, with the message id `read` gives
(`chat_message_id`, which `read` gives for a chat's messages; `ReactInChatAsAgent` holds it to the
rules of saying something there, and the person is asked first; a message of a chat the
person cannot read answers exactly like one that does not exist; the server's
own quick reaction is in the person's conversations with the agent on every
surface and not in a chat, so in a chat the agent's is the only one), and its fifth, `send`, says something in a chat under the agent's name
for the person (risk `outward`, so a card unless the person allowed it).

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
their beginning into a note. A chat is not swept either: it lasts until its
last member leaves it or its circle is deleted, and what was said in it is
never folded into a note.

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
