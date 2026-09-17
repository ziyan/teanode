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
turn sees the ones before it. Each turn it works, notes what it did, and
says one of four things through a tool: progress was made and it will look
again in so many minutes; the goal is met; it needs the person for
something and will wait for them; or nothing changed and it will wait
longer. The person sees the goal and its state on the conversation, in the
drawer, in the list and on the command line; they can change it, clear it,
or stop the run under way; and when the agent needs them or has finished
while the drawer is shut, it tells them the way a schedule does.

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
- [ ] Milestone 1: the goal on the conversation: columns, model, API,
  command line.
- [ ] Milestone 2: the goal tool and the goal job: the agent's own turns,
  their cadence, their limits, and how the person is told.
- [ ] Milestone 3: the drawer: set, see, change, clear, and the list mark;
  checked in Chrome at desktop and phone width.
- [ ] Milestone 4: tried on the dev server with a small goal, then on the
  maintainer's server with a real one; docs and retrospective.

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
- Observation: `models.AgentNotifications` (`internal/models/agent.go:134-146`:
  `HeldReply`, `HighPriority`, `RunFailed`, each off, dashboard or mail)
  is stored, edited in the dashboard, and read by nothing in
  `internal/agent`. The only delivery to a person that exists is the
  schedule's: mail to the account's notification address through a
  granted mailbox, or two messages appended to the main conversation.
- Observation: the job queue's uniqueness index `agent_job_open` on
  `(agent_id, kind, subject_id)` over open statuses
  (`internal/db/migrations/0041_agent_ownership.sql:22`) gives a goal job
  keyed by the conversation id exactly one run in flight per conversation
  with no extra code.

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
  `goal` tool it must call once a turn, and a turn that does not call it
  is treated as "nothing changed".
  Rationale: a fixed clock either wastes calls on a goal that waits for a
  nightly build or is too slow for one that watches a deploy. The agent
  knows which; the bounds keep it from spinning. The tool is also the one
  place the turn's outcome is written down in a shape the row can carry.
  Date/Author: 2026-09-17, the agent.
- Decision: a goal has a daily turn allowance of its own,
  `limits.goalTurnsPerDay`, beside the agent's cost budget, and pauses
  when it runs out.
  Rationale: the cost budget stops a runaway goal eventually; the turn
  allowance stops it before it has spent the day's budget on one
  conversation, and says so in a way the person can read.
  Date/Author: 2026-09-17, the agent.
- Decision: when the agent needs the person it pauses the goal and, if
  the drawer is shut, tells them the way a schedule delivers: by mail to
  the notification address when there is one, else nothing beyond the
  conversation's own state. The person's next turn in the conversation
  resumes the goal.
  Rationale: the only delivery that exists is the schedule's; building a
  notification system is its own plan. A paused goal with a visible state
  and a mail is enough for the first version.
  Date/Author: 2026-09-17, the agent.
- Decision: the goal is the person's word, set through the dashboard, the
  command line, or the `goal` tool when the person asks the agent to keep
  at something; the agent never sets a goal on its own initiative.
  Rationale: a goal spends the person's budget without them present. The
  tool's `set` is offered only in a turn the person started.
  Date/Author: 2026-09-17, the agent.

## Outcomes & Retrospective

To be written at the end of each milestone and at completion.

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
0040, 0067; the latest migration is 0082, so this plan's is 0083. Todos
(`models.AgentTodo`, `internal/models/memory.go:210-216`) are a child
table returned with the conversation, the nearest existing shape.

The API. In `internal/api/v1api/apigraph/agent_ask.go`: queries
`ListAgentConversations` (line 104), `ReadAgentConversation` (125)
returning `AgentConversationView{Conversation, Messages, Total, Todos,
ActingAs}` (132); mutations `AskAgent` (173), `StartAgentConversation`
(208), `UpdateAgentConversation(conversationId, title, archived)` (213,
implementation 871), `DeleteAgentConversation`, `StopAgentRun` (203),
`ResolveAgentConfirmation` (196); subscriptions `AgentConversationEvents`
(958). The command line's conversation commands are in
`internal/cmd/agent_chat.go` (`conversation list|show|new|rename|main|delete`,
lines 54-99) and the client in `internal/client`.

The turn. `Agent.Ask(settings *AskSettings)` (`internal/agent/ask.go:229-285`)
runs one turn; `AskSettings` (34-104) carries `Agent`, `Owner`,
`Operations`, `Conversation`, `Message`, `Surface`, `ReadOnly`, `Allow`,
`Headless`, `MaxRounds`, `UsageKind`, `Work`, `ReadThenAnswer`,
`ResultCharacters`, `Short`. A headless turn skips the `ask` feature gate
(241). Turns in one conversation run one at a time (266-272): a turn a
job starts while the person is typing waits its turn. Each round checks
the agent's budget (`RequireBudget`, `internal/agent/policy.go:229-246`)
and stops with a note when it is spent. A tool needing confirmation in a
headless turn is refused inline (1048-1060) rather than shown as a card.
`think` in `internal/agent/thinking.go:88-146` is how jobs run headless
turns today, always in a new `run` conversation; the goal turn is the
first headless turn in a person's own conversation.

Jobs. Kinds are in `internal/models/agent.go:391-444`; handlers are
registered in `internal/agent/agent.go:190-205`; `Enqueue` (278) dedupes
on `(agent, kind, subject)` for open jobs; `tickAt` (364-424) runs the
sweeps that queue work (`dueSchedules` first, line 381), then claims jobs
into free slots; a handler returning `*Deferral{Until, Reason}` (426-435)
puts the job back with `not_before` (471-473); other errors climb
`retryLadder` (339) and end as a dead letter. A job is bounded by ten
minutes (441-447).

Schedules, the nearest precedent. `models.AgentSchedule`
(`internal/models/memory.go:137-164`) with `Cron`, `Prompt`, `WrittenBy`,
`Deliver` (mail or drawer); `dueSchedules` (`internal/agent/schedule.go:51-93`)
queues a schedule job; `runSchedule` (96-160) runs a headless turn in a
fresh run transcript, framing an agent-written prompt as "not the person
speaking" (170-177); `deliverSchedule` (188-244) mails the answer to the
account's notification address through a granted mailbox, or appends a
`note` and an `assistant` message to the main conversation and bumps
`LastAt`. The dead-letter row "the account has no notification address to
mail the answer to" (194) is what a schedule leaves when it cannot mail.

The drawer. `web/src/components/agentDrawer.tsx`: the `Conversation`
type (62-69), the GraphQL documents (255-331, `UPDATE` at 318 sends only
title), the websocket feed (`subscribe(FEED)` at 1015, `web/src/api.ts:855`),
the header (1876-1932: conversation button, attached marks, budget ring,
close), the picker (1934-2060: search, new, sort with main pinned, rename,
make-main, delete). A person with the drawer open sees a headless turn in
their conversation live through the feed; with it shut they see nothing
until they open it.

Docs to update: `docs/subsystems/conversations.md`, `docs/subsystems/the-ask-loop.md`
("Cards and questions", "Every model call is a turn"),
`docs/subsystems/jobs-and-schedules.md` ("The kinds", "Schedules"),
`docs/reference/command-line.md`, `docs/configuration.md` for the new
limit.

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
      ADD COLUMN "goal_set_at" timestamptz,
      ADD COLUMN "goal_next_at" timestamptz,
      ADD COLUMN "goal_turns" integer NOT NULL DEFAULT 0,
      ADD COLUMN "goal_turns_on" date;
    CREATE INDEX "agent_conversation_goal_due"
      ON "agent_conversation" ("goal_next_at")
      WHERE "goal_state" = 'working';

`models.AgentConversation` gains `Goal string`, `GoalState
AgentGoalState`, `GoalNote string`, `GoalSetAt *time.Time`, `GoalNextAt
*time.Time`, `GoalTurns int`, `GoalTurnsOn *time.Time`, with

    type AgentGoalState string
    const (
        GoalWorking AgentGoalState = "working" // the agent takes turns on its own
        GoalWaiting AgentGoalState = "waiting" // it needs the person; their next turn resumes it
        GoalPaused  AgentGoalState = "paused"  // the day's turns are spent, or the budget
        GoalMet     AgentGoalState = "met"     // done; the goal text stays until cleared
    )

An empty `Goal` means none; `GoalState` is empty then. `GoalNote` is the
agent's last word on where it is, one or two sentences, for the drawer
and the list. `GoalTurns`/`GoalTurnsOn` count the agent's own turns today
for the allowance. The row model in `internal/db/database_insight.go`
(111-135) and `toModel` gain the columns; `UpdateAgentConversation` writes
them through the existing modify closure.

API: `UpdateAgentConversation` gains `goal *string`: a non-empty goal sets
`Goal`, `GoalState = working`, `GoalSetAt = now`, `GoalNextAt = now`,
`GoalNote = ""`; an empty string clears all of it. `StartAgentConversation`
gains `goal` too, so a goal can start a conversation. The conversation
type exposes the seven fields. `ListAgentConversations` needs no
argument; the drawer sorts and marks from the fields.

Command line, in `internal/cmd/agent_chat.go` under `conversation`:
`goal <conversation> "<text>"` sets, `goal <conversation> --clear` clears,
`goal` with no argument lists conversations with goals and their state,
note and next time; `conversation show` prints the goal line above the
messages when there is one. The client gains the fields and the argument.

Proof: `go test ./internal/db/ ./internal/models/ -count=1` with a test
that sets, reads and clears a goal through the store; `teanode agent
conversation goal <id> "count the unread mails"` then `conversation show`
prints `goal: count the unread mails (working)`.

## Milestone 2: the goal tool and the goal job

At the end of this milestone the agent takes turns toward a working goal
on its own, at a cadence it chooses within bounds, until it says the goal
is met, needs the person, or the day's allowance is spent; the person is
told when it stops and needs them.

The tool, `internal/agent/tools/goal/goal.go`, `Name: "goal"`, family
general, core, `Risk: tools.RiskWrite`, actions:

- `set` with `text`: sets the goal on this conversation as `working`.
  Offered only when the turn is not headless (the person is speaking), so
  the agent sets a goal when asked to "keep at this until…" and never on
  its own in a goal turn.
- `progress` with `note` and `minutes`: the turn did something; the note
  replaces `GoalNote`; the next turn is in `minutes`, clamped to
  `[goalSoonest, goalLatest]` = `[1 minute, 24 hours]`, default 30.
- `wait` with `note`: the agent needs the person (a confirmation it could
  not get, an answer, a file); state becomes `waiting`, the note says what
  it needs, and no turn is scheduled until the person's next turn.
- `unchanged` with `minutes`: nothing to do yet; like `progress` without a
  new note, and the interval doubles from the last one when `minutes` is
  omitted, up to `goalLatest`.
- `met` with `note`: the goal is met; state becomes `met`, the note is the
  closing word, no more turns.
- `clear`: the person asked to drop it in this turn; the goal is removed.

The tool writes the conversation row through `UpdateAgentConversation`
and, for `progress`, `unchanged` and `wait`, records what the next step
is in `GoalNextAt` (nil for `wait` and `met`).

The job, `models.AgentJobGoal = "goal"`, subject the conversation id,
registered beside the others. It is queued two ways. `dueGoals` in
`tickAt`, beside `dueSchedules`: `ListDueAgentGoals(now)` selects
conversations with `goal_state = 'working' AND goal_next_at <= now` (the
partial index above) and enqueues one job each; the dedupe on subject
makes a second sweep before the first job finishes a no-op. And at the
end of a person's own turn in a conversation whose goal is `waiting`, the
turn's `finish` sets the state back to `working` with `GoalNextAt = now +
goalAfterPerson` (one minute, so the person can keep typing), which the
next sweep picks up.

The handler, `runGoal` in `internal/agent/goal.go`:

1. Reads the conversation; if the goal is not `working` any more, done.
2. Allowance: if `GoalTurnsOn` is today and `GoalTurns >=
   configuration.Agent.Limits.GoalTurnsPerDay` (default 48), sets state
   `paused` with note "the day's N turns are spent; it goes on tomorrow,
   or when you write", schedules `GoalNextAt` at the owner's local
   midnight, and returns. Budget: `RequireBudget` as every job; a
   deferral here becomes `paused` with the budget's reason and
   `GoalNextAt = ResetsAt`.
3. Runs the turn: `Agent.Ask(&AskSettings{Agent, Owner, Operations:
   worker.OperationsFor(owner), Conversation: the person's, Message:
   goalCheckIn(conversation), Surface: "goal", Headless: true, UsageKind:
   "goal", MaxRounds: Limits.MaxRoundsPerAsk})`. The message is written as
   the framing `runSchedule` uses for agent-written prompts, so the model
   knows it is not the person speaking:

       [goal check-in, not the person speaking] The goal on this conversation
       is: <goal>. Your last note: <note>. It is <local time>. Work toward
       the goal with the tools you have; anything that needs the person's
       confirmation cannot be done now, so prepare it and use goal.wait.
       End by calling the goal tool once: progress, unchanged, wait or met.

   `Headless: true` refuses confirmation cards inline, as it does today,
   and the tool guidance tells the model to `wait` in that case.
4. Counts the turn: `GoalTurns` (reset when `GoalTurnsOn` is not today).
5. If the turn ended without a `goal` call, treats it as `unchanged`
   with the doubled interval, and after `goalSilentTurns` (3) such turns
   in a row sets `paused` with note "three turns without a word on the
   goal; say what to do next" so a model that ignores the tool does not
   loop.
6. Tells the person when the state left `working` in this turn (`waiting`,
   `paused`, `met`): if the account has a notification address and a
   granted mailbox, one mail with the note as its first line, the way
   `deliverSchedule` mails, with the subject "Goal: <first words>". If it
   cannot mail, nothing more: the state and note are on the conversation.
   The dead-letter row for a missing address is not repeated for goals;
   the mail is best effort and logged.

The person's controls are the existing ones: `UpdateAgentConversation(goal:
"")` clears and `StopAgentRun` stops a turn under way; clearing while a
turn runs also stops it, through `Agent.StopConversation`.

Prompt: `internal/agent/prompts/ask.txt` gains a paragraph under the
tools: when a conversation has a goal, its text and state are in the
system prompt's conversation section, and in a goal turn the model must
end with the goal tool. The `goal` tool's `Guidance` carries the rules
above in words.

Configuration: `limits.goalTurnsPerDay` (default 48) in
`internal/config/agent.go`, documented in `docs/configuration.md`, shown
on the agents page's Models tab beside the other limits.

Remember and compaction need nothing: goal turns are messages of the
conversation and the existing passes read them; `Surface: "goal"` lets
the remember prompt say which turns were the agent's own.

Proof: tests in `internal/agent/goal_test.go` with a fake provider whose
scripted answers call the tool: a `progress` schedules the next time in
bounds; a `wait` stops scheduling and the person's turn resumes it; three
silent turns pause; the allowance pauses at the limit and resumes at
midnight; `met` ends it. On the dev server, a goal "every check-in, note
how many unread mails there are and say the number" runs three turns
five minutes apart with the drawer showing them arrive.

## Milestone 3: the drawer

At the end of this milestone the person sets, sees, changes and clears a
goal from the drawer, on desktop and phone, and the list marks
conversations with a goal and its state.

In `web/src/components/agentDrawer.tsx`: the `Conversation` type and the
`CONVERSATION` and `CONVERSATIONS` documents gain `goal goalState goalNote
goalNextAt`; `UPDATE` gains `$goal`. In the header (1876-1932), beside
the conversation button, a goal chip: with no goal, an icon button with a
target icon (add `TargetIcon` to `web/src/components/icons.tsx`) and the
tooltip "Set a goal"; with one, a chip showing the state word (working,
waiting for you, paused, met) that opens the same dialog. The dialog
(`FormDialog`) has the goal text (a textarea), the current note read-only
under it when there is one, and two actions: Save, and Clear as the
`otherAction` at the far end. Setting a goal on a `met` conversation
starts it again.

In the picker rows (2032-2058), a target mark before the title for a
conversation with a goal, coloured by state (working: accent, waiting:
warn, met: muted), with the note as the row's second line when there is
one, so a list of conversations reads as a list of things under way.

In the transcript, a goal turn's opening is rendered as the check-in it
is: the framing line is shown muted, the way a schedule's `note` message
is, not as if the person wrote it. The feed already carries the turns
live.

i18n keys under `agentDrawer.goal.*` in en, zh and ja; `make
check-catalogs`. CSS for the chip and the mark in `web/src/style.css`
beside the drawer's rules. Checked in Chrome at 1600 and 420 wide before
deploy or commit, as every dashboard change is.

## Milestone 4: tried, documented, retrospected

On the dev server first: a goal that needs no confirmation ("note the
number of unread mails each check-in, and mark the goal met after the
third"), watched through three turns, the allowance set to 3 to see the
pause, then a goal that needs the person ("draft a reply to the newest
mail and wait for me to say send") to see `waiting`, the mail to the
notification address, and the resume on the person's turn. Then on the
maintainer's server with one real goal of their choosing, with the daily
allowance at its default. `docs/subsystems/conversations.md` gains a
section "A goal", `the-ask-loop.md` notes the goal surface among the
headless ones, `jobs-and-schedules.md` gains the `goal` kind and how it
differs from a schedule, `command-line.md` the `conversation goal`
command, `configuration.md` the limit. The retrospective records how many
turns the real goal took, what it cost, and what the person had to step
in for.

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

Setting the same goal twice is a no-op beyond `GoalSetAt`. A goal job
that finds the goal cleared does nothing. A turn stopped mid-way leaves
the transcript as it was and the next check-in reads it. The allowance
and the budget bound the worst case to one day's turns; clearing the goal
ends everything at once. The migration is additive with defaults and has
a reverse.

## Artifacts and Notes

To be filled: the transcript of the first dev goal, the mail a `wait`
sends, the retrospective's numbers.

## Interfaces and Dependencies

In `internal/models/insight.go`: `AgentGoalState` and its four constants,
the seven fields on `AgentConversation`. In `internal/models/agent.go`:
`AgentJobGoal`. In `internal/db`: migration 0083, the row columns,
`ListDueAgentGoals(now time.Time) ([]*models.AgentConversation, error)`.
In `internal/agent/goal.go`: `runGoal`, `dueGoals`, `goalCheckIn`,
constants `goalSoonest`, `goalLatest`, `goalDefaultInterval`,
`goalAfterPerson`, `goalSilentTurns`. In `internal/agent/tools/goal/goal.go`:
the tool with actions `set`, `progress`, `unchanged`, `wait`, `met`,
`clear`. In `internal/config/agent.go`: `Limits.GoalTurnsPerDay`. In
`internal/api/v1api/apigraph/agent_ask.go`: the `goal` argument on
`UpdateAgentConversation` and `StartAgentConversation`, the fields on the
conversation type. In `internal/cmd/agent_chat.go`: `conversation goal`.
In the dashboard: the chip, the dialog, the mark, `TargetIcon`. No new
libraries.

## Sources

Internal only. `docs/subsystems/conversations.md`, `docs/subsystems/the-ask-loop.md`,
`docs/subsystems/jobs-and-schedules.md`, `docs/subsystems/streaming-and-instances.md`,
and the plan `docs/planning/active/20260916-every-model-call-is-a-run.md`.
The shape of the check-in, a standing goal re-read at each wake with a
bounded, self-chosen cadence and an explicit "needs the person" state, is
the one the maintainer's own coding sessions use, and no published work
was drawn on.
