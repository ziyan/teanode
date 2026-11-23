# One goal for background work: schedules become goals

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds. It is maintained in accordance with the ExecPlan rules the maintainer keeps outside this repository; everything a contributor needs is written here.


## Purpose / Big Picture

TeaNode has two things that make the agent act with nobody asking: a **schedule** (a name, a cron line or the mail it waits for, a prompt, and where the answer goes) and a **goal** (a conversation of its own that the agent keeps at until it is met). They look alike from the queue and are kept apart everywhere else: two tables, two tools, two sets of operations, two command groups, two sections on every Goals screen. The seam shows most where they meet. A goal whose work is on a clock owns a schedule, and the goal then takes no turns of its own while the schedule takes them; two rows say when the agent next looks, and the goal tool has `schedule_ids` and `take_schedule` only to keep them in step. Most schedules on a working server end up owned by a goal this way.

After this change there is one thing, the goal. A goal can repeat at times the person chose ("weekdays at 7:30"), wait for mail ("when a message from the school arrives"), happen once at a moment, or, as today, let the agent decide when it looks next. The person sees one list, each goal with what it is for, when it next looks, and a switch to pause it. The daily brief and the asset estimates are goals their switches start. Schedules, the schedule tool, the schedule operations, `teanode agent schedule` and the Schedules sections are removed.

To see it working: ask in the main conversation "every weekday at 7:30, tell me what the day holds". The agent starts a goal titled for it, and the Goals tab shows it with "Weekdays at 7:30" and its next time. At 7:30 its turn runs in the goal's own conversation and posts to the feed. Pause it on the Goals tab and it does not run; resume it and the next time is the next weekday at 7:30. `teanode agent goal list` shows the same.


## Progress

- [x] (2026-11-22) Surveyed schedules and goals end to end (`internal/agent/schedule.go`, `schedule_mail.go`, `goal.go`, `goal_background.go`, the goal and schedule tools, the operations, the command line and the three apps) and wrote this plan.
- [ ] Milestone 1: storage. Migration moves every schedule onto a goal and drops `agent_schedule`.
- [ ] Milestone 2: the worker. Goals repeat, wait for mail, run once; the schedule sweep and job kind are removed.
- [ ] Milestone 3: the goal tool, the operations, the client and the command line.
- [ ] Milestone 4: the dashboard.
- [ ] Milestone 5: the iPhone app.
- [ ] Milestone 6: the Android app.
- [ ] Milestone 7: docs, review, deploy, tester builds.


## Surprises & Discoveries

(none yet)


## Decision Log

- Decision: a goal keeps being a conversation of kind `goal` and takes the schedule's fields as columns of its own; there is no new table.
  Rationale: the decision of 2026-10-05 (`background-goals-execplan.md`) still holds: the job, the caps, the back-off and the transcript the next turn reads already work per conversation. What was duplicated was the schedule beside it, not the goal's storage.
  Date/Author: 2026-11-22.
- Decision: a time the person chose is kept as a repeat rule the worker applies, a cron line in the person's zone (`goal_repeat`); a goal without one keeps its cadence from the agent's own `note`, as now.
  Rationale: the rule for what is stored is "what the platform must apply without a model, or what the person must see and change". "Weekdays at 7:30" is both: the brief's switch has a time and days, and a brief that arrives at 8:12 because a model counted wrong is a regression a person notices. A goal the agent paces for itself ("watch for the refund") needs no rule and gets none.
  Date/Author: 2026-11-22.
- Decision: a goal for one moment is a goal with that `goal_next_at`, no repeat, marked met after its turn unless the turn set a next look.
  Rationale: `@at` and `@in` schedules were removed once they ran; a met goal stays in Done, which is where the person looks for what happened.
  Date/Author: 2026-11-22.
- Decision: mail conditions and the arrivals they collect move to the goal (`goal_mail_conditions`, `goal_mail_arrivals`). A goal waiting for mail with no repeat has no `goal_next_at` between messages.
  Rationale: the same delivery hook and the same run's opening lines, keyed by the goal instead of a schedule.
  Date/Author: 2026-11-22.
- Decision: the switch is a goal state, `paused`. A disabled schedule becomes a paused goal. A paused goal takes no turns and collects no mail; working again computes the next time from now.
  Rationale: one field the person changes, and the Goals screens already group by state.
  Date/Author: 2026-11-22.
- Decision: what a goal tells the person goes to the feed, which `agent.feed_destination` already sends to the main conversation for a person who chose that. Mail as a destination is removed; `is_notified_when_posted` moves to the goal.
  Rationale: the feed replaced mail and the drawer as the place the agent's own work arrives. A goal's turns already run in its own conversation, so "answer in the drawer" has nowhere else to mean.
  Date/Author: 2026-11-22.
- Decision: who wrote the instruction moves to the goal (`goal_written_by`: person, agent or teanode) and decides, as for schedules, whether the check-in carries it as the person's words or fenced as a note.
  Rationale: a goal runs with nobody watching; an instruction the agent wrote on the strength of something it read must not arrive as the person asking.
  Date/Author: 2026-11-22.
- Decision: a goal with a repeat rule or mail conditions is outside the forty-eight a day and twenty-four without the person caps, as schedules were.
  Rationale: its cadence is the person's choice, not the agent's; a daily brief would otherwise stall on its twenty-fifth day. Replacing the caps with the spending limit is a separate change.
  Date/Author: 2026-11-22.


## Context and Orientation

The server is Go (`internal/`), the dashboard React (`web/src/`), the iPhone app SwiftUI (`ios/`), the Android app Kotlin and Compose (`android/`). The worker in `internal/agent/agent.go` ticks every few seconds and queues due work as jobs (`agent_job`). `dueSchedules` (`internal/agent/schedule.go`) queues a `schedule` job per due schedule; `dueGoals` (`internal/agent/goal.go`) a `goal` job per goal conversation that is `working` and past `goal_next_at`. `runSchedule` takes a headless turn in the schedule's conversation, or a `run` transcript when it mails; `runGoal` takes one in the goal's conversation and reads back what the goal tool's `note`, `wait` or `met` wrote. `noteMailForSchedules` (`internal/agent/schedule_mail.go`) runs inside mail delivery and makes matching schedules due.

A goal's fields are columns of `agent_conversation`: `goal` (its description), `goal_title`, `goal_state` (`working`, `waiting`, `met`, `dropped`), `goal_note`, `goal_next_at`, `goal_set_at`, `goal_surfaced_at` and the origin conversation. Its activity and artifacts are `agent_goal_activity` and `agent_goal_artifact`.

The daily brief (`internal/api/v1api/apigraph/agent_brief.go`) and the asset estimates (`agent_finance_estimate.go`) are switches that write a schedule found by its name.


## Plan of Work

Milestone 1. Migration `0281_goal_takes_schedules.sql` adds to `agent_conversation` `goal_repeat` (text, empty), `goal_written_by` (text, empty), `goal_mail_conditions` (jsonb, null), `goal_mail_arrivals` (jsonb, null) and `is_goal_notified_when_posted` (boolean, false). Each schedule in a goal conversation, the first by creation where there are several, fills that goal's new columns, its prompt is added to the goal's description under "Each time:", and its next run becomes the goal's next time when sooner. Every other schedule becomes a new goal conversation: the same agent and owner, its name as title, its prompt as description, its cron line as the repeat unless it is a moment, its next run as the goal's next time, `working` when enabled and `paused` when not, and the conversation it was made in as the origin. `agent_schedule` is dropped. The reverse recreates `agent_schedule` as it was and writes a schedule back for every goal with a repeat or mail conditions, then drops the columns.

Milestone 2. `models.AgentConversation` gains the fields; `GoalPaused` joins the states. `SettleSchedule` becomes `settleGoalRepeat`, the next cron time in the owner's zone. `dueGoals` keeps its query, which already skips everything but `working`. `runGoal` adds the arrivals and the repeat to the check-in, and afterwards: a goal with a repeat takes its next time from the rule whatever the turn said, a goal for one moment that set nothing is met, a goal waiting for mail with nothing set has no next time. `noteMailForSchedules` becomes `noteMailForGoals`. `runSchedule`, `dueSchedules`, the `schedule` job kind and the schedule's mail and drawer delivery are removed. The brief and asset-estimate switches start or change a goal found by its title.

Milestone 3. The goal tool's `start` takes `repeat` (a cron line), `at` (a moment in the person's zone) and `on_mail` (conditions); `note` takes `at` beside `minutes`; a new `set` changes a goal's title, text, repeat, conditions or notification; `pause` and `resume` are the person's. `schedule_ids` and `take_schedule` are removed, and with them the schedule tool. Operations: `StartAgentGoal` and a new `UpdateAgentGoal` carry the new fields, `SetAgentGoalState` accepts `paused` and `working`, `RunAgentGoal` looks now; `ListAgentSchedules`, `SaveAgentSchedule`, `DeleteAgentSchedule`, `RunAgentSchedule` and `MoveAgentScheduleToGoal` are removed. `SetAgentBrief` returns the goal. The command line: `teanode agent goal start|set|pause|resume|run` gain the flags; `teanode agent schedule` is removed. The parity test maps every argument.

Milestones 4 to 6. On the dashboard, iPhone and Android the Goals screen shows one list; a goal row shows when it next looks and how it repeats; the start and edit forms reuse the schedule editors' timing and mail-condition pieces; the Schedules section and screens are removed; the brief's settings row links to its goal.

Milestone 7. `docs/subsystems/jobs-and-schedules.md` (its goal and schedule sections become one), `docs/reference/command-line.md`, a decision record, review until a round finds nothing, deploy with a backup first, tester builds from integration.


## Validation and Acceptance

Database tests: the migration turns a goal's schedule into the goal's repeat and its prompt into the description, a standalone schedule into a goal, a disabled one into a paused goal, a moment into a goal with that time and no repeat; the reverse brings them back. Agent tests: a repeating goal's next time follows the rule after a turn that asked for five minutes; a paused goal is not queued; a goal for one moment is met after its turn; mail that passes a goal's conditions makes it due and its run is told; a repeating goal is not stalled at twenty-four turns. Operation tests: a person sees and changes only their own goals; the brief's switch makes and changes one goal. The three apps' tests and screenshots show the list and the editor.

By hand on the server, after deploy: the existing schedules appear as goals with their times; the brief arrives at its time; a goal is paused and resumed from the phone.


## Idempotence and Recovery

Back up the database before the deploy that applies the migration. The forward migration runs once. The reverse writes back a schedule for every goal with a repeat or mail conditions; it cannot tell a goal made from a schedule from one started with a repeat, so both come back as schedules, which is what they would have been.


## Interfaces and Dependencies

In `internal/models`:

    const GoalPaused AgentGoalState = "paused"
    // on AgentConversation
    GoalRepeat               string
    GoalWrittenBy            string
    GoalMailConditions       []MailboxRuleCondition
    GoalMailArrivals         []AgentGoalArrival
    IsGoalNotifiedWhenPosted bool

GraphQL:

    StartAgentGoal(goalTitle, goalDescription, goalRepeat, goalAt, goalMailConditions, isGoalNotifiedWhenPosted): AgentConversation
    UpdateAgentGoal(conversationId, goalTitle, goalDescription, goalRepeat, goalMailConditions, isGoalNotifiedWhenPosted): AgentConversation
    SetAgentGoalState(conversationId, goalState: working|paused|met|dropped): AgentConversation
    RunAgentGoal(conversationId): AgentConversation
