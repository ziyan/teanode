# A goal on a conversation: the agent keeps working toward it across turns until it is met, and the person can set, see and clear it

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to
date as work proceeds. `~/.claude/PLAN.md` describes the form this document
takes; keep it in accordance with that file. It builds on
`docs/planning/active/20260916-every-model-call-is-a-run.md`, checked in,
which put every model call through the conversation loop; everything this
plan needs from it is repeated here.

## Purpose / Big Picture

Today a conversation with the agent is a series of turns the person starts.
Each turn ends when the agent answers, and nothing happens until the person
writes again. Work that takes more than one turn, "find every invoice from
last quarter and file them", "keep an eye on the deploy and tell me when
the build is green", "draft the reply, wait for the numbers I will paste,
then send it", has to be driven by the person, turn after turn, or handed
to a schedule, which runs a fresh transcript on a clock with no memory of
its last run and no way to say it is done.

After this plan a conversation can carry a *goal*: a sentence the person
sets, or asks the agent to set, that stays on the conversation until it is
met or cleared. While a goal is set, the agent takes turns in that
conversation on its own, in the same transcript the person reads, so every
turn sees the ones before it. Each turn it works, and ends by saying one
of three things through a tool: here is where I am and when I will look
again; I need you, and will wait; or the goal is met. The person sees the
goal and its state on the conversation, in the drawer, in the list and on
the command line; they can change it, clear it, or stop the turn under
way; and when the agent needs them or has finished while the drawer is
shut, it tells them the way a schedule does.

A person sees it working like this. In the drawer they press the goal
button on a conversation, type "reply to every mail from the landlord
this week; ask me before sending anything", and close the drawer. Over
the next hour the conversation grows by three turns of the agent's own: it
found two mails, drafted two replies, and stopped with "I need you: two
drafts are ready, say send or edit". The drawer's list shows the
conversation with a target mark and the words "waiting for you". They
open it, say "send both", and the agent sends them, marks the goal met,
and the mark goes.

## Progress

- [x] (2026-09-17 12:00Z) Research: mapped the conversation model, the
  turn loop and its headless path, confirmation cards without a person,
  the job queue and its dedupe, schedules end to end, the drawer and the
  command line. Findings in Context and Orientation.
- [x] (2026-09-17 12:40Z) Reviewed for simplicity at the maintainer's ask:
  four columns rather than seven, three tool outcomes rather than six,
  three states rather than four, no new setting, no daily allowance
  column, no special rendering of the check-in. Recorded in the Decision
  Log.
- [x] (2026-09-17 12:05Z) Milestone 1: the goal on the conversation:
  migration 0083 and its reverse, the four fields and `AgentGoalState` on
  the model, the row columns and `ListDueAgentGoals`, the `goal` argument
  on `UpdateAgentConversation` and `StartAgentConversation`, the client
  fields, and `teanode agent conversation goal`. A store test covers
  setting, reading, listing as due and clearing.
- [x] (2026-09-17 12:11Z) Milestone 2: the `goal` tool with its four
  actions, the `goal` job kind and handler, `dueGoals` in the tick, the
  check-in with its marker, the day's cap, the budget deferral, the
  silent-turn doubling, the resume on the person's turn, the mail when it
  stops, and the goal in the prompt's situation. Five tests with a
  scripted provider and a unit test for the doubling.
- [x] (2026-09-17 12:23Z) Milestone 3: the drawer: the four fields on the
  conversation documents and `goal` on `UPDATE`, `TargetIcon`, the target
  button and the state chip in the head with the goal as its tooltip, the
  `FormDialog` with the note read-only and Clear as its other action, the
  two toasts, the coloured mark in the picker rows, the
  check-in drawn as one muted line, and the waiting note above the
  composer. Checked in Chrome on the dev server and on the maintainer's
  server (2026-09-17 13:35Z): the icon-only chip in its three colours,
  the dialog's status list reading `met · since 9:22 AM · 1 turn today ·
  last note`, the check-in line opening on its prompt.
- [x] (2026-09-17 12:11Z) Milestone 4, the documents:
  `conversations.md` gains "A goal", `jobs-and-schedules.md` the `goal`
  kind and the table of how it differs from a schedule, `the-ask-loop.md`
  the one headless turn that runs in a person's own conversation, and
  `command-line.md` the `conversation goal` command. Trying it on the dev
  server and the maintainer's server, and the retrospective, wait for
  Milestone 3.

## Surprises & Discoveries

- Observation (2026-09-17 17:40Z): a goal at the five-minute cadence kept
  the night from starting. The dream's quiet rule read the newest
  conversation's `LastAt`, which every turn writes, the agent's own
  included, so while the turn-bound test ran no dream began for a
  quarter of an hour. The rule now asks when the person last wrote
  (`LastAgentPersonWordAt`: their words in their own conversations, not
  a check-in), which is what "not while they are talking" meant.

- Observation: no existing path runs a turn *inside* a person's
  conversation without the person. A schedule runs a headless turn in a
  fresh `run` transcript and then appends its answer to the main
  conversation as two messages (`internal/agent/schedule.go:96-160` and
  `:188-244`), so successive runs never see each other. The loop itself
  does not care who started a turn: `Agent.Ask` takes any conversation,
  and the per-conversation run queue (`internal/agent/ask.go:266-272`)
  already serializes a turn started by a job behind one the person is
  typing.
- Observation: in a headless turn a tool that needs confirmation is not
  offered a card at all; it is refused inline with
  `needs_confirmation: nobody is present to confirm this; tell the person
  what you would have done` (`internal/agent/ask.go:1048-1060`). A goal
  turn therefore cannot send, delete or pay on its own, which is the
  right default, and the only way to reach the person is the words of
  the answer and whatever delivers them.
- Observation: `models.AgentNotifications` (`internal/models/agent.go:134-146`)
  is stored, edited in the dashboard, and read by nothing in
  `internal/agent`. The only delivery to a person that exists is the
  schedule's: mail to the account's notification address through a
  granted mailbox, or two messages appended to the main conversation.
- Observation: the job queue's uniqueness index `agent_job_open` on
  `(agent_id, kind, subject_id)` over open statuses
  (`internal/db/migrations/0041_agent_ownership.sql:22`) gives a goal job
  keyed by the conversation id exactly one run in flight per conversation
  with no extra code, and the finished rows the queue keeps until it
  scavenges them are a count of today's goal turns without a column.
- Observation (2026-09-17 12:05Z): `db.AgentJobFilter` could not say what
  a job was about, nor from when. Counting today's goal turns from the job
  rows -- which is what stands in for a column -- wanted both, so the
  filter gained `SubjectID` and `Since`; every existing caller passes
  neither and is unaffected.
- Observation (2026-09-17 12:11Z): the interval a goal is running at is
  not kept anywhere, so a turn that says nothing has nothing obvious to
  double. It is recoverable from the row: the turn before wrote its next
  time when it ended, so the gap between the row's `modified_at` and its
  `goal_next_at` is the interval that turn asked for. A goal just set has
  the same moment in both, and the default stands in. No column for it.
- Observation (2026-09-17 12:11Z): the schedule's mail delivery was the
  only way this program tells a person anything, and it was written inside
  `deliverSchedule`. It is now `mailToPerson`, which the goal uses too;
  nothing about the schedule's behaviour changed.

- Observation: the first real goal on the maintainer's server (2026-09-17
  12:37Z, "list the open pull requests and failed runs from the GitHub
  folder, then mark the goal met") wrote its table in twelve seconds and
  never called the goal tool, so the row said nothing about when to look
  again and the silent-turn rule put the next look an hour out. The dev
  goal, on the same model, had called it every time. The handler now asks
  once more when a turn ends without the tool, with the goal tool alone
  in front of the model and two rounds; a check-in is also numbered ("the
  2nd today") because the model, told to mark a goal met after the second
  check-in, took a third.

## Decision Log

- Decision (2026-09-17 20:10Z): the bound's stop is a stall, said in the
  transcript. The state stays `waiting` -- no fourth state -- but the
  note reads "Goal stalled: 24 turns since you last wrote and it is not
  met. Write to keep going, or clear or change it.", that line is
  appended to the conversation, and the mail's subject is "Goal stalled:
  …".
  Rationale: the maintainer, after the bound fired on the test goal:
  "maybe we should say goal failed? And let user clear it or change it?"
  The goal was never cleared by the bound, and the dialog already offers
  clear and change; what was missing was any trace in the conversation,
  since a wait adds no line and here no turn ran to say anything. Stalled
  rather than failed because the agent does not know the goal is
  impossible, only that nothing moved while nobody watched.

- Decision (2026-09-17 16:20Z): a goal stops after twenty-four turns of its
  own with no word from the person, set to `waiting` with a note asking
  whether to go on, and they are mailed as for any wait; writing resumes
  it and restarts the count, which is taken from the job rows since the
  later of the goal's setting and their last message.
  Rationale: the maintainer asked that an unachievable goal not burn
  tokens indefinitely. The day's cap bounded a day, and the doubling only
  a turn that said nothing: a goal that answered "look again in five
  minutes" to a build that never goes green would have cost forty-eight
  turns a day for as long as nobody looked. The person's own word is the
  right reset because it is the one signal that somebody is still
  watching.

- Decision (2026-09-17 15:00Z): the goal's beginning and end are lines of
  the transcript. Setting, changing and clearing a goal, and the agent
  calling `met`, each append a note to the conversation -- "Goal set:
  …", "Goal changed: …", "Goal cleared: …", "Goal met: …" with the
  agent's note -- drawn as the muted line a schedule's run or a stop
  already is. A `note` or a `wait` does not: the chip and the bar carry
  those, and a line for every check-in would be the clutter the
  simplification took out.
  Rationale: the maintainer, reading the transcript back: the goal lived
  in a dialog and a chip, so the conversation showed the agent working
  toward something it never said, and ending without saying it was done.
  The wording is one function, `models.GoalChangeNote`, called from the
  dialog's path, a new conversation's, and the tool's, so the three say
  the same thing.

- Decision (2026-09-17 13:50Z): the picker row's second line is the time
  of the last message on every row, goal or not; the goal's note is read
  in the dialog.
  Rationale: the maintainer, seeing "Discarded the saved draft; nothing
  was sent." where every other row said "12 hours ago": the time is what
  tells the rows apart, and a row that swaps it for a sentence breaks the
  list's one column of like with like.

- Decision: the goal lives on the conversation row, as text with a state,
  not as a schedule and not as a todo.
  Rationale: a schedule has a clock and no end; a todo has an end and no
  agency. A goal is the conversation's own standing instruction, and the
  turns it drives belong in the same transcript so they see each other and
  the person reads them where they read everything else.
  Date/Author: 2026-09-17, the agent, from the maintainer's ask.
- Decision: the agent's own turns are headless turns in the person's
  conversation, with surface `goal`, on the person's own model.
  Rationale: headless is what refuses confirmation cards when nobody is
  there, and the person's model is what the conversation runs on; a goal
  turn is the conversation continuing, not a scan.
  Date/Author: 2026-09-17, the agent.
- Decision: the cadence is the agent's to choose within bounds, through a
  `goal` tool it ends each turn with, and a turn that does not call it
  waits twice as long as the last.
  Rationale: a fixed clock either wastes calls on a goal that waits for a
  nightly build or is too slow for one that watches a deploy. The agent
  knows which; the bounds keep it from spinning.
  Date/Author: 2026-09-17, the agent.
- Decision (simplified 2026-09-17 12:40Z): three states, `working`,
  `waiting`, `met`; three tool outcomes, `note`, `wait`, `met`, plus `set`
  in a person's turn; four columns; the bound on runaway goals is the
  agent's cost budget plus a floor of five minutes between turns and a
  cap of forty-eight of the agent's own turns a day per conversation,
  counted from the job rows, all as constants. The earlier draft had a
  `paused` state, an `unchanged` action, a daily allowance with two
  columns and a setting, a silent-turn counter, and a special rendering
  of the check-in in the transcript.
  Rationale: the maintainer asked whether the plan was overcomplicated,
  and it was. A budget that runs out already stops the turn with a note
  the transcript shows; a goal that says nothing is a goal that waits
  longer, which needs no state; a cap that is a constant needs no docs,
  no settings page and no migration column, and can become a setting the
  day somebody wants a different number. The check-in's framing line
  already says it is not the person; a different look for it is polish
  for later.
  Date/Author: 2026-09-17, the agent, with the maintainer.
- Decision: when the agent needs the person, or has met the goal, and the
  drawer is shut, it tells them the way a schedule delivers: by mail to
  the notification address when there is one, else nothing beyond the
  conversation's own state. The person's next turn in the conversation
  resumes a waiting goal.
  Rationale: the only delivery that exists is the schedule's; building a
  notification system is its own plan.
  Date/Author: 2026-09-17, the agent.
- Decision: the goal is the person's word, set through the dashboard, the
  command line, or the `goal` tool when the person asks the agent to keep
  at something; the agent never sets a goal in a goal turn.
  Rationale: a goal spends the person's budget without them present.
  Date/Author: 2026-09-17, the agent.

- Decision (2026-09-17 12:11Z): the marker that opens a check-in is
  `models.GoalCheckInMarker`, `[goal check-in]`, exactly, and the rest of
  the framing follows it.
  Rationale: the maintainer asked that the dashboard be able to draw the
  agent's own turn as a muted "Goal check-in" line rather than as the
  person's bubble, which needs a marker both sides agree on. It is in
  `models` rather than in `internal/agent` because the API hands the same
  transcript to the dashboard; Go will not export a lowercase name, so it
  is `GoalCheckInMarker` rather than the `goalCheckInMarker` the ask
  named.
  Date/Author: 2026-09-17, the agent, from the maintainer's ask.
- Decision (2026-09-17 12:11Z): the bounds on the cadence live in
  `internal/agent/tools/goal`, where they are enforced, and
  `internal/agent/goal.go` names them as the plan does.
  Rationale: the tool clamps what the model asks for, and the handler
  doubles a silent turn's wait against the same numbers. A tool package
  cannot import the agent package, so one of the two had to hold them;
  holding them where the clamp happens keeps the check and the number in
  one file.
  Date/Author: 2026-09-17, the agent.
- Decision (2026-09-17 12:11Z): a goal turn whose budget is spent, or
  whose day's cap is reached, finishes the job and writes the next time on
  the conversation rather than returning a `Deferral`.
  Rationale: what the plan says, and the reason it is right here is that
  attempts are counted at claim time and never reset, so a goal deferred
  five times over a month's budget would dead-letter on its first real
  failure. The cost is that such a job counts toward the day's cap; it is
  written down in the caveats.
  Date/Author: 2026-09-17, the agent.

- Decision (2026-09-17 13:05Z): no set-a-goal button in the drawer. The
  agent sets and clears the goal from what the person says, through the
  tool's `set`; the drawer shows the goal that exists and offers Clear.
  Rationale: the maintainer, seeing the button, said the agent should set
  the goal according to the person's input; a form for a sentence the
  person would type into the composer anyway is a second way to say one
  thing.
  Date/Author: 2026-09-17, the maintainer.

## Outcomes & Retrospective

**The stall, on the maintainer's server (2026-09-17 22:48Z).** The
unmeetable goal was run a second time under the stall wording. Twenty-four
turns, none failed; the twenty-fifth job asked the model nothing, set the
goal waiting, wrote "Goal stalled: 24 turns since you last wrote and it is
not met. Write to keep going, or clear or change it." as the transcript's
last line, and one mail went out under "Goal stalled: …". The goal was then
cleared. Seen along the way, unasked: given a one-sentence request to
index an export, the agent set a goal on that conversation by itself,
"add and verify ingestion", which the tool's description tells it not to
do; whether that is a wording problem or a judgment the person is glad of
is for more real use to say.

**Done.** Every milestone is in, tried on the dev server and the
maintainer's, and documented; the plan moves to done with the pull
request that carries it.

**The bound on turns alone, on the maintainer's server (2026-09-17
18:50Z).** A conversation was given a goal that says it cannot be met and
asks for a five-minute note every turn. Twenty-four turns ran from 16:39Z
to 18:38Z, each calling the tool once, none failing; the twenty-fifth job
ran at 18:43Z, asked the model nothing, set the goal `waiting` with "24
turns on this since you last wrote, and it is not met; write if I should
keep going", and one mail went out with the goal as its subject. One
message from the person resumed it: the next turn ran a minute later and
the goal was working again, because the count restarts from their word.
The goal was then cleared. Cost of the whole run: about thirty cents.
Seen along the way: the dream's quiet rule read the conversation's last
message time, which the goal's own turns kept fresh, so no night began
while the goal ran; the rule now reads the person's last word.

**Second real goal, end to end (2026-09-17 13:35Z).** "Draft a short,
friendly reply to the newest mail in my Inbox thanking the sender, save it
as a draft, then wait for me to say whether to send it. Do not send
anything." The check-in searched, drafted, and called `wait` with a
one-line note; the mail for `waiting` went to the notification address
and the bar above the composer showed the note. The person's next turn
resumed the goal, the agent discarded the draft and called `met`; no mail
went out for `met`, as decided. Two things it turned up. The agent threw
the draft away with `mail_act trash`, which moved it to Trash, where the
dashboard's Discard deletes a draft for good -- so `mail_draft` gained a
`discard` mode that finds the draft by the key it keeps across saves and
deletes it as the composer does, and `mail_act trash` deletes rather than
moves any draft among its items. And the first try at `mail_act trash`
failed on the `draft_id` it had just been given, because a draft's item
id names one save and the id `mail_draft` answers is the key: `discard`
takes the key, which is what the model holds.

**First real goal (2026-09-17 12:50Z).** On the maintainer's server, a new
conversation was given the goal "look at my GitHub folder: list the open
pull requests and failed workflow runs the mails there mention from the
last two days, in one short table; then mark the goal met". The first
check-in ran twelve seconds after the goal was set, searched the folder
twice, wrote the table (60k tokens in, 513 out, $0.13 on the person's
model) and ended without the goal tool; the second, brought forward by
hand and running under the follow-up ask, called `met` with a one-line
note, and the drawer showed the chip go from "working · next look 8:37 AM"
to "met" with the conversation titled by the describer. No mail went out:
the account has no notification address, which is the condition the mail
waits on, and nothing else says so to the person. The dev trial before it
ran a counting goal through two turns of its own and one of the person's,
with `waiting` and `met` shown from rows set by hand. Open: a check-in on
a paid model carries the whole conversation each time, so a goal looking
every five minutes at a long conversation would be dear; compaction bounds
it, and the five-minute floor may want raising once more real goals have
been seen.

**Milestones 1 and 2 (2026-09-17 12:11Z).** The row, the API, the command
line, the tool, the job and the documents are in. What the plan expected
held: the per-conversation run queue serializes a goal turn behind one the
person is typing with no new code, the dedupe on the subject makes the
five-second sweep idempotent, and the finished job rows are a usable count
of the day's turns. Three things the plan did not say are written down
under Surprises: the job filter needed a subject and a since, the running
interval is recoverable from the row rather than needing a column, and the
schedule's mail path is now shared. Nothing is proved on a server yet: the
dev-server run and the maintainer's real goal belong with the drawer, in
Milestones 3 and 4, since a goal nobody can see or clear from the
dashboard is not something to point at a real mailbox.

## Context and Orientation

TeaNode is a mail server with a personal agent, in Go under `internal/`,
with a React dashboard under `web/` and PostgreSQL behind it. Terms used
below: a *conversation* is a transcript between the person and their
agent; a *turn* is one exchange in it, the person's message and everything
the agent does until it answers; a *headless* turn is one nobody is
watching, started by a job; a *job* is a unit of background work in the
queue; a *run* is a conversation of kind `run` that a job created for its
own model calls; the *drawer* is the chat panel in the dashboard.

The conversation. `models.AgentConversation` (`internal/models/insight.go:126-170`)
has `ID`, `AgentID`, `Kind` (`main`, `named`, `run`), `Title`, `Summary`,
`Surface` (where the last turn came from: drawer, page, cli, api, mail),
`ArchivedAt`, `LastAt`, `CompactedThrough`, `RememberedThrough`, and
nothing per conversation beyond those; the person's model choice is on
the agent (`models.Agent.AskModel`, `internal/models/agent.go:52-54`). The
row is `agent_conversation`, made in migration 0032 and extended in 0039,
0040, 0067; the latest migration is 0082, so this plan's is 0083.

The API. In `internal/api/v1api/apigraph/agent_ask.go`: queries
`ListAgentConversations` (line 104), `ReadAgentConversation` (125)
returning `AgentConversationView{Conversation, Messages, Total, Todos,
ActingAs}` (132); mutations `AskAgent` (173), `StartAgentConversation`
(208), `UpdateAgentConversation(conversationId, title, archived)` (213,
implementation 871), `DeleteAgentConversation`, `StopAgentRun` (203);
subscriptions `AgentConversationEvents` (958). The command line's
conversation commands are in `internal/cmd/agent_chat.go`
(`conversation list|show|new|rename|main|delete`, lines 54-99) and the
client in `internal/client`.

The turn. `Agent.Ask(settings *AskSettings)` (`internal/agent/ask.go:229-285`)
runs one turn; `AskSettings` (34-104) carries `Agent`, `Owner`,
`Operations`, `Conversation`, `Message`, `Surface`, `ReadOnly`, `Allow`,
`Headless`, `MaxRounds`, `UsageKind`, `Work`, `ReadThenAnswer`,
`ResultCharacters`, `Short`. A headless turn skips the `ask` feature gate
(241). Turns in one conversation run one at a time (266-272). Each round
checks the agent's budget (`RequireBudget`, `internal/agent/policy.go:229-246`)
and stops with a note in the transcript when it is spent. A tool needing
confirmation in a headless turn is refused inline (1048-1060). `think` in
`internal/agent/thinking.go:88-146` is how jobs run headless turns today,
always in a new `run` conversation; the goal turn is the first headless
turn in a person's own conversation.

Jobs. Kinds are in `internal/models/agent.go:391-444`; handlers are
registered in `internal/agent/agent.go:190-205`; `Enqueue` (278) dedupes
on `(agent, kind, subject)` for open jobs; `tickAt` (364-424) runs the
sweeps that queue work (`dueSchedules` first, line 381), then claims jobs
into free slots; a handler returning `*Deferral{Until, Reason}` (426-435)
puts the job back with `not_before`; other errors climb `retryLadder`
(339) and end as a dead letter; `CountAgentJobs(filter)`
(`internal/db/database_agent.go:621`) counts rows by agent, kind and
status. A job is bounded by ten minutes (441-447).

Schedules, the nearest precedent. `dueSchedules` (`internal/agent/schedule.go:51-93`)
queues a schedule job; `runSchedule` (96-160) runs a headless turn in a
fresh run transcript, framing an agent-written prompt as "not the person
speaking" (170-177); `deliverSchedule` (188-244) mails the answer to the
account's notification address through a granted mailbox, or appends two
messages to the main conversation.

The drawer. `web/src/components/agentDrawer.tsx`: the `Conversation`
type (62-69), the GraphQL documents (255-331, `UPDATE` at 318 sends only
title), the websocket feed (`subscribe(FEED)` at 1015), the header
(1876-1932), the picker (1934-2060). A person with the drawer open sees a
headless turn in their conversation live through the feed.

Docs to update: `docs/subsystems/conversations.md`, `docs/subsystems/the-ask-loop.md`,
`docs/subsystems/jobs-and-schedules.md`, `docs/reference/command-line.md`.

## User experience

What the person sees and does, before any file is named. Everything below
is checked in Chrome at 1600 and 420 wide before it is deployed.

Setting a goal. The person says it: "keep working on this until the
deploy is green", "check every hour whether the build is fixed and tell
me", "drop the goal". The agent calls the goal tool's `set` from the
person's words, or clears it the same way, and its answer says what it
set. There is no button for it: a goal is a thing asked for in the
conversation, and a form beside the composer for a sentence the person
would type into the composer anyway was a second way to say one thing
(the maintainer's call, 2026-09-17 13:05Z, after seeing the button).

Seeing it. With a goal set, a chip in the drawer's header carries the
state: "working · next look 10:42", "waiting for you", or "met". The
chip's tooltip is the goal's text. Pressing the chip opens a small
dialog showing the goal, its state and the agent's last note, with one
action, Clear, the stop control a person may want without a word; with
no goal there is nothing in the header. In the conversation
list, a conversation with a goal shows a small target mark before its
title, in the accent colour while working, the warning colour while
waiting, muted when met, and the agent's note as the row's second line
where the summary would be. On the phone the chip shrinks to the icon in
the state's colour, and the state word is in its tooltip and in the
dialog.

The agent's own turns. They arrive in the transcript live when the
drawer is open, through the feed every turn already uses. The check-in
that opens each of them is not the person's words and must not look like
them: a user message beginning with the marker `[goal check-in]` is drawn
as a single muted line, "Goal check-in · 10:42", where a person's bubble
would be, and the agent's answer under it looks like any answer. Nothing
else in the transcript changes.

Needing the person. When the agent calls `wait`, the chip turns to
"waiting for you" and the note it left, say "two drafts are ready: send,
edit, or drop?", appears in a muted bar directly above the composer, so
the person sees what is wanted where they are about to type. Their next
message clears the bar and the goal goes back to working. With the drawer
shut, the same note is the first line of a mail to the notification
address, subject "Goal: two drafts are ready", when the account has one.

Met. The icon goes muted, the dialog's state says "met" with the agent's
closing word as the last note, the mark in the list goes muted; no mail. The goal text stays until the person clears it,
so they can read what was done and set the next one from the same dialog.

Stopping. Clear in the dialog, or telling the agent to drop the goal,
removes it and stops a turn under way; the dialog shows "Goal cleared".
The existing stop button on a running turn still works and does not
clear the goal.

Errors. A goal that cannot run, because the day's budget is spent or the
cap of forty-eight turns is reached, is not an error the person is shown
as one: the chip stays "working", the note says why it is waiting and
until when, and the next look time on the chip says when.

Command line. `teanode agent conversation goal <id> "…"` prints "goal set
on <title>"; `--clear` prints "goal cleared"; `goal` alone prints a
table of conversations, states, next look times and notes; `conversation
show` prints the goal line first.

## Milestone 1: the goal on the conversation

At the end of this milestone a conversation can carry a goal, set and
cleared through the API and the command line, and read back with its
state; nothing acts on it yet.

Migration `internal/db/migrations/0083_agent_conversation_goal.sql` (with
its `.reverse.sql`):

    ALTER TABLE "agent_conversation"
      ADD COLUMN "goal" text NOT NULL DEFAULT '',
      ADD COLUMN "goal_state" text NOT NULL DEFAULT '',
      ADD COLUMN "goal_note" text NOT NULL DEFAULT '',
      ADD COLUMN "goal_next_at" timestamptz;
    CREATE INDEX "agent_conversation_goal_due"
      ON "agent_conversation" ("goal_next_at")
      WHERE "goal_state" = 'working';

`models.AgentConversation` gains `Goal string`, `GoalState
AgentGoalState`, `GoalNote string`, `GoalNextAt *time.Time`, with

    type AgentGoalState string
    const (
        GoalWorking AgentGoalState = "working" // the agent takes turns on its own
        GoalWaiting AgentGoalState = "waiting" // it needs the person; their next turn resumes it
        GoalMet     AgentGoalState = "met"     // done; the text stays until cleared
    )

An empty `Goal` means none, and `GoalState` is empty then. `GoalNote` is
the agent's last word on where it is, for the drawer and the list. The
row model in `internal/db/database_insight.go` (111-135) and `toModel`
gain the columns; `UpdateAgentConversation` writes them through the
existing modify closure; `ListDueAgentGoals(now)` returns conversations
with `goal_state = 'working' AND goal_next_at <= now`.

API: `UpdateAgentConversation` gains `goal *string`: a non-empty goal sets
`Goal`, `GoalState = working`, `GoalNextAt = now`, `GoalNote = ""`; an
empty string clears all four. `StartAgentConversation` gains `goal` too.
The conversation type exposes the four fields.

Command line, in `internal/cmd/agent_chat.go` under `conversation`:
`goal <conversation> "<text>"` sets, `goal <conversation> --clear` clears,
`goal` alone lists conversations with goals, their state and note;
`conversation show` prints the goal line above the messages when there is
one. The client gains the fields and the argument.

Proof: a store test that sets, reads, lists as due and clears a goal;
`teanode agent conversation goal <id> "count the unread mails"` then
`conversation show` prints `goal: count the unread mails (working)`.

## Milestone 2: the goal tool and the goal job

At the end of this milestone the agent takes turns toward a working goal
on its own, at a cadence it chooses within bounds, until it says the goal
is met or needs the person; the person is told when it stops.

The tool, `internal/agent/tools/goal/goal.go`, `Name: "goal"`, family
general, core, `Risk: tools.RiskWrite`, actions:

- `set` with `text`: sets the goal on this conversation as `working`, or
  clears it when `text` is empty. Offered only when the turn is not
  headless, so the agent sets or drops a goal when the person asks and
  never in a goal turn.
- `note` with `text` and `minutes`: where the agent is and when to look
  again; the text replaces `GoalNote`, and the next turn is in `minutes`,
  clamped to `[goalSoonest, goalLatest]` = `[5 minutes, 24 hours]`,
  default 30.
- `wait` with `text`: the agent needs the person (a confirmation it could
  not get, an answer, a file); state becomes `waiting`, the text says what
  it needs, and no turn is scheduled until the person's next turn.
- `met` with `text`: the goal is met; state becomes `met`, the text is the
  closing word, no more turns.

The tool writes the row through `UpdateAgentConversation`.

The job, `models.AgentJobGoal = "goal"`, subject the conversation id,
registered beside the others. `dueGoals` in `tickAt`, beside
`dueSchedules`, enqueues one job per due conversation; the dedupe on
subject makes a second sweep a no-op while a job is open. At the end of a
person's own turn (in `loop`, after `turn` returns) in a conversation
whose goal is `waiting`, the state goes back to `working` with
`GoalNextAt = now + goalAfterPerson` (one minute).

The handler, `runGoal` in `internal/agent/goal.go`:

1. Reads the conversation; if the goal is not `working`, done.
2. Cap: `CountAgentJobs` of kind `goal` with this subject and status
   `done` since the owner's local midnight; at `goalTurnsPerDay` (48) it
   sets `GoalNextAt` to the next midnight with the note "forty-eight
   turns today; it goes on tomorrow, or when you write" and returns.
   Budget: `RequireBudget`; a deferral sets `GoalNextAt = ResetsAt` with
   the budget's reason as the note and returns.
3. Runs the turn: `Agent.Ask(&AskSettings{Agent, Owner, Operations,
   Conversation: the person's, Message: goalCheckIn(conversation),
   Surface: "goal", Headless: true, UsageKind: "goal", MaxRounds:
   Limits.MaxRoundsPerAsk})`, with the message framed the way
   `runSchedule` frames an agent-written prompt:

       [goal check-in, not the person speaking] The goal on this conversation
       is: <goal>. Your last note: <note>. It is <local time>. Work toward
       the goal with the tools you have; anything that needs the person's
       confirmation cannot be done now, so prepare it and call goal.wait.
       End by calling the goal tool once: note, wait or met.

4. If the turn ended without a `goal` call, sets `GoalNextAt` to twice
   the last interval (kept as the gap between the turn and the previous
   `GoalNextAt`), capped at `goalLatest`.
5. When the state became `waiting` in this turn and the account has a
   notification address and a granted mailbox, one mail with the note as
   its first line, the way `deliverSchedule` mails, subject "Goal: <first
   words>"; best effort, logged, never a dead letter. A goal that is met
   is not mailed about (the maintainer's ask, 2026-09-17 13:20Z): the
   mark in the drawer and the closing note are there when they next look.

The person's controls are the existing ones: `UpdateAgentConversation(goal:
"")` clears, and clearing also stops a turn under way through
`Agent.StopConversation`; `StopAgentRun` stops one turn.

Prompt: the conversation section of the system prompt carries the goal
and its state when there is one, and the tool's `Guidance` carries the
rules above in words. No new setting; the bounds are constants in
`internal/agent/goal.go`.

Proof: tests in `internal/agent/goal_test.go` with a scripted provider: a
`note` schedules the next time in bounds; a `wait` stops scheduling and
the person's turn resumes it; a silent turn doubles the interval; the
cap defers to midnight; `met` ends it. On the dev server, a goal "every
check-in, note how many unread mails there are and say the number" runs
three turns at the intervals it chose with the drawer showing them
arrive.

## Milestone 3: the drawer

At the end of this milestone the person sets, sees, changes and clears a
goal from the drawer, on desktop and phone, and the list marks
conversations with a goal and its state.

In `web/src/components/agentDrawer.tsx`: the `Conversation` type and the
`CONVERSATION` and `CONVERSATIONS` documents gain `goal goalState goalNote
goalNextAt`; `UPDATE` gains `$goal`. In the header (1876-1932), beside
the conversation button, a goal control: with no goal, an icon button
with a target icon (add `TargetIcon` to `web/src/components/icons.tsx`)
and the tooltip "Set a goal"; with one, a chip with the state word
(working, waiting for you, met) that opens the same dialog. The dialog
(`FormDialog`) has the goal text, the current note read-only under it
when there is one, Save, and Clear as the `otherAction`. Setting a goal on
a `met` conversation starts it again.

In the picker rows (2032-2058), a target mark before the title for a
conversation with a goal, coloured by state. The row's second line stays
the time of the last message, on every row.

In the transcript, a user message that begins with the `[goal check-in]`
marker (the exact string `goalCheckInMarker` the handler writes, exported
by the API as a constant the dashboard imports from its generated
schema, or simply matched by prefix) is drawn as one muted line, "Goal
check-in · time", not as a bubble. While the goal is `waiting`, the note
is shown in a muted bar above the composer; it goes when the person
sends.

i18n keys under `agentDrawer.goal.*` in en, zh and ja; `make
check-catalogs`. CSS for the chip and the mark in `web/src/style.css`
beside the drawer's rules. Checked in Chrome at 1600 and 420 wide before
deploy or commit, as every dashboard change is.

## Milestone 4: tried, documented, retrospected

On the dev server first: a goal that needs no confirmation ("note the
number of unread mails each check-in, and mark the goal met after the
third"), watched through three turns; then a goal that needs the person
("draft a reply to the newest mail and wait for me to say send") to see
`waiting`, the mail to the notification address, and the resume on the
person's turn. Then on the maintainer's server with one real goal of
their choosing. `docs/subsystems/conversations.md` gains a section "A
goal", `the-ask-loop.md` notes the goal surface among the headless ones,
`jobs-and-schedules.md` gains the `goal` kind and how it differs from a
schedule, `command-line.md` the `conversation goal` command. The
retrospective records how many turns the real goal took, what it cost,
and what the person had to step in for.

## Concrete Steps

All commands run from the repository root.

    go build ./... && go vet ./internal/agent/ ./internal/db/ ./internal/models/ ./internal/cmd/
    make lint-ci
    TEANODE_TEST_DATABASE_HOST=172.17.0.9 go test ./internal/db/ ./internal/agent/ -count=1
    cd web && npx tsc --noEmit -p . && npx prettier --check src

Migrations go under `internal/db/migrations/` as `0083_agent_conversation_goal.sql`
with a `.reverse.sql`, following `docs/coding/database-migrations.md`.
The dev server is `make build` then `./build/teanode-server run` with
`dev/.env` in the environment, dashboard at `http://127.0.0.1:10081`.
Deploy to the maintainer's server with `make docker DOCKER_TAG=teanode:memory`,
`docker save teanode:memory | ssh root@server docker load`, `ssh root@server
'cd /opt/teanode && docker compose up -d --force-recreate teanode'`,
after backing up `agent_conversation` (`pg_dump -t agent_conversation`)
since the migration touches it. Every deploy ends the running dream, so
batch them.

## Validation and Acceptance

After Milestone 1, `teanode agent conversation goal <id> "x"` followed by
`conversation show` prints the goal and `working`; `--clear` removes it;
the reverse migration restores the table. After Milestone 2, with a goal
set on the dev server and the drawer open, turns the agent started appear
in the transcript at the intervals it chose, the row's note changes, and
`wait` stops the turns until the person writes. After Milestone 3, the
drawer shows and edits the goal at both widths and the list marks it.
After Milestone 4, a real goal on the maintainer's server reaches `met`
or `waiting` with the person told by mail, and the retrospective has the
numbers.

## Idempotence and Recovery

Setting the same goal twice is a no-op. A goal job that finds the goal
cleared does nothing. A turn stopped mid-way leaves the transcript as it
was and the next check-in reads it. The budget, the five-minute floor and
the daily cap bound the worst case; clearing the goal ends everything at
once. The migration is additive with defaults and has a reverse.

## Artifacts and Notes

To be filled: the transcript of the first dev goal, the mail a `wait`
sends, the retrospective's numbers.

## Interfaces and Dependencies

In `internal/models/insight.go`: `AgentGoalState` and its three constants,
the four fields on `AgentConversation`. In `internal/models/agent.go`:
`AgentJobGoal`. In `internal/db`: migration 0083, the row columns,
`ListDueAgentGoals(now time.Time) ([]*models.AgentConversation, error)`.
In `internal/agent/goal.go`: `runGoal`, `dueGoals`, `goalCheckIn`,
constants `goalSoonest`, `goalLatest`, `goalDefaultInterval`,
`goalAfterPerson`, `goalTurnsPerDay`. In `internal/agent/tools/goal/goal.go`:
the tool with actions `set`, `note`, `wait`, `met`. In
`internal/api/v1api/apigraph/agent_ask.go`: the `goal` argument on
`UpdateAgentConversation` and `StartAgentConversation`, the fields on the
conversation type. In `internal/cmd/agent_chat.go`: `conversation goal`.
In the dashboard: the chip, the dialog, the mark, `TargetIcon`. No new
libraries, no new settings.

## Sources

Internal only. `docs/subsystems/conversations.md`, `docs/subsystems/the-ask-loop.md`,
`docs/subsystems/jobs-and-schedules.md`, `docs/subsystems/streaming-and-instances.md`,
and the plan `docs/planning/active/20260916-every-model-call-is-a-run.md`.
The shape of the check-in, a standing goal re-read at each wake with a
bounded, self-chosen cadence and an explicit "needs the person" state, is
the one the maintainer's own coding sessions use, and no published work
was drawn on.
