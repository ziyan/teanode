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
- [ ] Milestone 3: the drawer: set, see, change, clear, and the list mark;
  checked in Chrome at desktop and phone width.
- [x] (2026-09-17 12:11Z) Milestone 4, the documents:
  `conversations.md` gains "A goal", `jobs-and-schedules.md` the `goal`
  kind and the table of how it differs from a schedule, `the-ask-loop.md`
  the one headless turn that runs in a person's own conversation, and
  `command-line.md` the `conversation goal` command. Trying it on the dev
  server and the maintainer's server, and the retrospective, wait for
  Milestone 3.

## Surprises & Discoveries

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

## Decision Log

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

## Outcomes & Retrospective

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
5. When the state left `working` in this turn (`waiting` or `met`) and
   the account has a notification address and a granted mailbox, one
   mail with the note as its first line, the way `deliverSchedule` mails,
   subject "Goal: <first words>"; best effort, logged, never a dead
   letter.

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
conversation with a goal, coloured by state, with the note as the row's
second line when there is one.

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
