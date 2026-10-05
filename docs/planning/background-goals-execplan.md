# Background goals: work the agent keeps at between conversations, and brings to the person only when they are needed

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds.


## Purpose / Big Picture

An always-on personal agent is asked for things that take days: "keep the login-code mail out of my inbox", "watch for the refund and tell me if it does not come", "get the inbox under twenty by Friday". Today TeaNode can keep at such a thing only as a goal on a conversation, and every turn it takes toward it is written into that conversation's transcript, often the main one the person chats in. A goal that looks every half hour fills the person's chat with "still nothing new", and each of those turns is also relayed to their linked chat apps.

After this plan a goal is background work with a life of its own. The person (or the agent, when the person asks it to keep at something) starts a goal; it gets a short title, a description of what it is for, and a conversation of its own where its turns run out of the person's sight. It keeps a one-line status ("Waiting for the person to confirm the new bank link was theirs"), an activity log of what happened ("Instagram login-code texts added to the sweep", Sep 30), and the things it made along the way: schedules, background work, mail rules, reminders and alert mutes. The main conversation hears from a goal only when the person is needed: one message saying which goal and what it needs. The person answers right there in the main chat, and the agent passes the answer to the goal, which carries on. Finished goals simply move to Done.

To see it working: in the main conversation ask "keep an eye out for a reply from the landlord about the boiler and tell me if it hasn't come by Friday". The agent starts a goal and says so in one line. The Goals tab lists it under Tracking with its status line; opening it shows the description, an activity log and, if the agent made a schedule for it, that schedule under Artifacts. Nothing more appears in the main conversation while it works. When it needs the person, one message appears in the main conversation naming the goal and the question; answering there moves the goal on, and the answer shows in its activity. `teanode agent goal list` and `teanode agent goal show <id>` show the same.


## Progress

- [x] (2026-10-05) Surveyed every mechanism that works between conversations (goals, todos, reminders, schedules and the brief, alerts, mail rules, ideas, speaking first, background work, the reply queue, interactions, statement imports, mail to the person, the chat-app relay) and wrote this plan.
- [ ] Milestone 1: storage. Migration 0148: the `goal` conversation kind, `goal_title` and the `dropped` state on conversations, `agent_goal_activity`, `agent_goal_artifact`. Models and database methods.
- [ ] Milestone 2: the goal runs out of sight. Starting a goal makes its conversation; goal turns run there; every change writes an activity row; goal turns may start background work; tools that make a mail rule, a reminder or an alert mute in a goal conversation record it as an artifact.
- [ ] Milestone 3: surfacing and answering. A goal that comes to need the person (it waits, or stalls) says so once in the main conversation; the main turn's prompt carries the goals waiting on the person; the goal tool's `tell` passes the person's answer on and starts the goal again.
- [ ] Milestone 4: the API, the tool and the command line. `ListAgentGoals`, `GetAgentGoal`, `StartAgentGoal`, `TellAgentGoal`, `SetAgentGoalState`; the goal tool's actions over them; `teanode agent goal list|show|start|tell|done|drop`.
- [ ] Milestone 5: the dashboard. The Goals tab grouped as Needs you, Tracking and Done, each with its status line; a goal's page with the description, artifacts and activity, a box to answer, and Done and Drop; the main conversation draws a goal's call for attention as a card that links to it.
- [ ] Milestone 6: documentation, tests, review, deploy, and a check in a browser at desktop and phone widths.


## Surprises & Discoveries

- Nothing yet beyond the survey. Its facts that shape this plan: a goal today is five columns on `agent_conversation` (`goal`, `goal_state`, `goal_note`, `goal_next_at`, `goal_set_at`, migrations 0083 and 0087), run by the `goal` job in `internal/agent/goal.go`, which already has the turn caps (48 a day, 24 without the person), the doubling back-off, budget deferral and resuming when the person writes. Background work refuses to start from a turn with nobody present (`startBackgroundWork` in `internal/agent/background_work.go`), so a goal turn can start none. Alerts are written into the main conversation by `deliverAlert` in `internal/agent/alert.go`, which is the pattern a goal's call for attention follows. On the maintainer's server there is one goal, met, on a named conversation, and three schedules.


## Decision Log

- Decision: a goal is a conversation of a new kind, `goal`, and keeps the existing goal columns. Its title is a new column, `goal_title`; its description is the existing `goal` text; its status line is the existing `goal_note`.
  Rationale: every piece of the goal machinery (the job, the caps, the back-off, resuming, the turn's transcript that the next turn reads) already works per conversation. A conversation of its own gives the goal's turns a transcript the person does not read unless they open it, which is exactly "in the background". A separate goal table would duplicate all of it.
  Date/Author: 2026-10-05.
- Decision: starting a goal always makes a new goal conversation; a goal is no longer put on the main conversation or a named one. Goals already on a named or main conversation keep running under the old rules until they end.
  Rationale: the point is that the person's chat stays theirs. There is one such goal on the maintainer's server and it is met, so no data migration is needed.
  Date/Author: 2026-10-05.
- Decision: what happened is kept as rows in `agent_goal_activity`: when, what kind (started, progress, waiting, resumed, met, dropped, stalled, failed), a headline and a detail. A turn writes a progress row only when it says something happened (`activity` on the tool call); a turn that only looked and found nothing updates the status line and writes no row.
  Rationale: the log is for the person to read; forty-eight "nothing new" rows a day would bury the three that matter.
  Date/Author: 2026-10-05.
- Decision: the things a goal made are its artifacts. Schedules and background work already carry the conversation they were made in, so a goal's are found by its conversation. Mail rules, reminders and alert mutes carry no conversation, so the tools that make them record a row in `agent_goal_artifact` (kind, the artifact's own id or name, and a title) when they run in a goal conversation.
  Rationale: no new columns on tables that other features own, and nothing to keep in step when a schedule moves.
  Date/Author: 2026-10-05.
- Decision: a goal reaches the main conversation only when it needs the person: when a turn says `wait`, and when it stalls after 24 turns alone. It writes one exchange there, as an alert does: a marker line `[goal needs you]` naming the goal, and the agent's sentence saying what it needs. Meeting a goal, progress and failures do not post; they are in the goal's activity and status. The mail that goal waits already send is kept.
  Rationale: the person asked for the main conversation to carry only what needs a human. A met goal is good news that can wait for them to look; a failure is retried with back-off and stalls, which then does post.
  Date/Author: 2026-10-05.
- Decision: the person answers where they are. The main turn's prompt lists the goals waiting on the person (id, title, what each needs), and the goal tool's `tell` passes the person's words to one, written into its conversation as a message from the person relayed from the main conversation, with a `resumed` activity row; the goal goes back to work a minute later. Writing in the goal's own conversation, from the Goals tab, resumes it as before.
  Rationale: "yes, that was me" belongs in the chat the question appeared in; making the person go to another page to say it is the friction this is meant to remove.
  Date/Author: 2026-10-05.
- Decision: a goal turn may start background work, which wakes the goal's conversation when it finishes, never the main one.
  Rationale: long work is what goals are for, and the wake lands where the goal's turns run, so the person is not disturbed.
  Date/Author: 2026-10-05.
- Decision: a new state, `dropped`, for a goal the person stopped. Only the person drops a goal or reopens one; the agent's own turns can only say note, wait or met.
  Rationale: Done should distinguish "it happened" from "never mind", and the agent must not abandon what the person asked it to keep at.
  Date/Author: 2026-10-05.
- Decision: alerts, schedules made in the main conversation, speaking first and statement imports keep posting to the main conversation as they do. They are already either something the person asked to receive there (a schedule, an import) or the agent's judgement that the person should know now (an alert). The Goals tab keeps listing standalone schedules beside goals, so every between-conversations task is in one place.
  Rationale: changing their delivery is a separate decision with its own trade-offs; this plan consolidates where they are seen and gives long-running work a place that is not the chat.
  Date/Author: 2026-10-05.


## Outcomes & Retrospective

Not yet.


## Context and Orientation

TeaNode is a mail server with an AI agent for each person. The server is Go (`internal/`), the dashboard React (`web/src/`), the database PostgreSQL. Code that talks to the model lives in `internal/agent`; the tools the model can call are packages under `internal/agent/tools/`; the GraphQL API is in `internal/api/v1api/apigraph/`, with one Go method per operation (the operation's name is the method's name, such as `ListAgentConversations`); the command line is `internal/cmd/`, calling the same operations through documents in `internal/client/`. A tool that acts across conversations calls the API through `operator.Execute` so the tool, the dashboard and the command line behave the same.

A conversation (`models.AgentConversation`, table `agent_conversation`) is a transcript of messages. Its kind is `main` (the person's one ongoing chat, shown in the drawer), `named` (a separate chat the person started) or `run` (the record of a headless run). A turn is one request to the model and everything it does until it answers; `Ask` in `internal/agent/ask.go` runs one. A headless turn is one with nobody present (a schedule, a goal check-in); `Surface` names what started it.

The worker (`internal/agent/agent.go`) ticks every few seconds; each tick queues due work as jobs (`agent_job`), and handlers run them. `dueGoals` in `internal/agent/goal.go` queues a `goal` job for every conversation whose goal is working and due; `runGoal` takes one turn in that conversation with a check-in message that begins `[goal check-in]` and asks the model to end by calling the goal tool (`internal/agent/tools/goal/goal.go`) with `note` (where it is and minutes until the next look), `wait` (it needs the person) or `met`.

An alert (`internal/agent/alert.go`) is the agent telling the person something unprompted; `deliverAlert` writes a marker line and the alert text into the main conversation and publishes events so an open drawer shows it. `OwnTurnMarkers` in `internal/models/insight.go` lists the markers of turns the agent takes on its own; `internal/channel/relay.go` forwards those turns in the main conversation to the person's linked chat apps.

Migrations are numbered SQL files in `internal/db/migrations/` with a `.reverse.sql` beside each; `docs/coding/database-migrations.md` says how to add one. The next free number is 0148.


## Plan of Work

Milestone 1 adds storage. Migration `0148_agent_goals.sql` adds `goal_title` to `agent_conversation`, allows the kind `goal` and the goal state `dropped` wherever the schema constrains them, and creates `agent_goal_activity` (id, agent_id, conversation_id, created_at, goal_activity_kind, activity_headline, activity_detail) and `agent_goal_artifact` (id, agent_id, conversation_id, created_at, goal_artifact_kind, artifact_reference, artifact_title), both deleted with their conversation. The models gain `AgentConversationGoal`, `GoalDropped`, `AgentGoalActivity` and `AgentGoalArtifact`; the database gains methods to add and list both, and to list goal conversations by state.

Milestone 2 moves goals out of sight. A function `StartGoal` in `internal/agent/goal.go` makes a goal conversation (kind `goal`, its title the goal's title, `TitledBy` "program"), writes the description, the origin and the person's request into it as its first message, sets the goal working and due now, and writes a `started` activity row. The goal tool's `note`, `wait` and `met` each write the status line, and a row when they say something happened. `runGoal` writes `stalled` and `failed` rows where it now writes notes. `startBackgroundWork` accepts a turn whose surface is the goal's when its conversation is a goal conversation. The mail rule, reminder and alert mute tools record an artifact when the turn's conversation is a goal conversation.

Milestone 3 surfaces and answers. `surfaceGoal` writes, under the same lock and transaction pattern as `deliverAlert`, a `[goal needs you]` marker line and the goal's need into the main conversation, publishes the events, and lets the relay forward it (the marker joins `OwnTurnMarkers`). `runGoal` calls it where it now mails a waiting goal, and the stall path calls it too. The prompt of a turn in the main conversation gains a short section listing the goals waiting on the person, with their ids. The goal tool's `tell` writes the person's words into the goal's conversation as a person message prefixed `[from the person, in the main conversation]`, adds a `resumed` row, and sets the goal working with its next turn in a minute.

Milestone 4 gives every surface the same operations. `ListAgentGoals(goalStates)` lists goal conversations with title, state, status line and next time; `GetAgentGoal(conversationId)` adds the description, activity and artifacts (schedules and background work by conversation, the rest from `agent_goal_artifact`); `StartAgentGoal(goalTitle, goalDescription)` starts one; `TellAgentGoal(conversationId, text)`; `SetAgentGoalState(conversationId, goalState)` for met, dropped, or working again. The goal tool's actions become `start`, `list`, `show`, `tell`, `note`, `wait`, `met`, and for the person `done` and `drop`; `set` remains only to clear an old conversation goal. The command line gets `teanode agent goal list|show|start|tell|done|drop`.

Milestone 5 is the dashboard: `web/src/pages/agentGoals.tsx` lists goals in three groups with the status line under each title and keeps the schedules section; a goal's page shows the description, Artifacts and Activity, a box to answer it, and Done, Drop and Open conversation; the drawer draws a `[goal needs you]` exchange as a card with a link to the goal.

Milestone 6 updates `docs/subsystems/` (jobs and schedules, the ask loop), the command-line reference and a decision record, then reviews, deploys and checks the pages in a browser at 1400 and 390 pixels.


## Concrete Steps

Run from the repository root. Build and unit tests: `go build ./...`, `go vet ./internal/...`. Database tests need PostgreSQL with pgvector; `make test` starts one, or start `pgvector/pgvector:pg17` and export `TEANODE_TEST_DATABASE_HOST` as the Makefile does, then `go test -race ./internal/agent/ ./internal/db/ -run Goal`. The dashboard: `cd web && npx tsc --noEmit -p . && npx vitest run`. Lint as CI does: `make lint-ci`.


## Validation and Acceptance

Tests in `internal/agent` show: starting a goal makes a goal conversation and a `started` row, and nothing is written to the main conversation; a goal turn that says `note` with an activity writes a row and one without writes none; a turn that says `wait` writes one `[goal needs you]` exchange into the main conversation and a `waiting` row, and a second wait while still waiting writes no second exchange; `tell` from the main conversation writes the person's words into the goal conversation, a `resumed` row, and makes the goal due a minute later; a stall surfaces; met and dropped do not; a goal turn can start background work, which wakes the goal conversation; a mail rule made in a goal turn is listed among the goal's artifacts. Tests in `internal/api/v1api/apigraph` show the operations' permissions (a person sees and changes only their own goals) and shapes. The dashboard tests show the three groups and the card.

Acceptance by hand, on a dev server: ask in the drawer for something to keep at; see the goal start and the drawer stay quiet; open the Goals tab and see it under Tracking with a status line; force a wait (`teanode agent goal show` then let a turn ask a question) and see one message in the drawer; answer it there and see the goal's activity gain `resumed` and the goal go back to Tracking.


## Idempotence and Recovery

The migration only adds columns, tables and allowed values, and its reverse drops them; goal conversations made meanwhile would then read as an unknown kind, so the reverse also turns them into named conversations. Starting a goal twice makes two goals; the tool's description tells the model to `list` first. Surfacing is guarded by the goal's state, so a retried job does not post twice.


## Interfaces and Dependencies

In `internal/models`:

    const AgentConversationGoal AgentConversationKind = "goal"
    const GoalDropped AgentGoalState = "dropped"
    type AgentGoalActivityKind string // started, progress, waiting, resumed, met, dropped, stalled, failed
    type AgentGoalActivity struct { ID, AgentID, ConversationID string; CreatedAt time.Time; GoalActivityKind AgentGoalActivityKind; ActivityHeadline, ActivityDetail string }
    type AgentGoalArtifactKind string // mail_rule, reminder, alert_mute
    type AgentGoalArtifact struct { ID, AgentID, ConversationID string; CreatedAt time.Time; GoalArtifactKind AgentGoalArtifactKind; ArtifactReference, ArtifactTitle string }
    const GoalNeedsYouMarker = "[goal needs you]"

In `internal/db` (`db.Transaction`):

    AddAgentGoalActivity(activity *models.AgentGoalActivity) (*models.AgentGoalActivity, error)
    ListAgentGoalActivity(agentId, conversationId string, limit int) ([]*models.AgentGoalActivity, error)
    AddAgentGoalArtifact(artifact *models.AgentGoalArtifact) (*models.AgentGoalArtifact, error)
    ListAgentGoalArtifacts(agentId, conversationId string) ([]*models.AgentGoalArtifact, error)
    ListAgentGoals(agentId string, goalStates []models.AgentGoalState, limit int) ([]*models.AgentConversation, error)

In `internal/agent`:

    func (self *Agent) StartGoal(ctx context.Context, agent *models.Agent, owner *models.User, goalTitle, goalDescription, originConversationId string) (*models.AgentConversation, error)
    func (self *Agent) TellGoal(ctx context.Context, agent *models.Agent, conversationId, text string) (*models.AgentConversation, error)
    func (self *Agent) SetGoalState(ctx context.Context, agent *models.Agent, conversationId string, goalState models.AgentGoalState) (*models.AgentConversation, error)
