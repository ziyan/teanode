# Background work: surveys and subagents that run on and wake the conversation that started them

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds.

## Purpose / Big Picture

A survey takes minutes, and a subagent going through a repository can take ten. Today both run inside the turn that asked for them: the person watches the turn sit on one tool call, the turn cannot do anything else meanwhile, and a request from the command line or the API holds one HTTP request open for the whole time, which a proxy with a short timeout cuts off. Shell commands had the same problem and solved it: a command can run in the background, the turn ends, and when the command ends the conversation that started it is woken with the result (`docs/decisions/20260923-a-background-command-wakes-the-conversation-that-started-it.md`).

After this plan the agent can start a survey or a subagent in the background. The tool call returns at once with an id; the turn goes on or ends; when the work finishes, the conversation is woken with a turn that begins `[background work]` and carries the result, and the agent tells the person what came of it. The dashboard, the command line and the agent can all list background work, read one, and stop one. `teanode agent survey` starts a survey and waits for it by asking for its result, not by holding one request open, so it works behind any proxy.

To see it working: in the drawer ask "survey my notes on the garden in the background and tell me when it's done"; the agent answers at once that it has started; a minute or two later a new turn appears with the report. `teanode agent background list` shows it while it runs.

## Progress

- [x] (2026-09-29) Read how background shell commands wake a conversation (`internal/agent/background.go`) and how jobs run; wrote this plan.
- [ ] Milestone 1: the record and the job: `agent_background_work`, a job kind that runs one, stopping, and the result kept.
- [ ] Milestone 2: waking the conversation when it finishes, sharing the bounds background commands have.
- [ ] Milestone 3: the tools, the API and the command line: survey and subagent with `background`, list/read/stop everywhere, `teanode agent survey` by start and wait.
- [ ] Milestone 4: the dashboard, docs, a decision record, deploy, and a check end to end.

## Surprises & Discoveries

None yet.

## Decision Log

- Decision: background work is a row in a new table, `agent_background_work`, run by a queued job whose subject is the row.
  Rationale: shell commands survive a server restart because the person's computer holds them. A survey or a subagent runs on the server, and a goroutine dies with a deploy; a queued job is claimed again after a restart (at least once, as the job queue already guarantees). Jobs carry only a subject id, so what was asked (the question and scope, or the prompt and title), where to wake, and the result need a row of their own. The row is also what the API and the command line read the result from.
  Date/Author: 2026-09-29.
- Decision: the woken turn follows the same rules as a background command's: only work started in a turn the person took wakes anything; a conversation takes at most twenty woken turns, from commands and work together, before the person writes again; out of turns or out of budget, the result is written into the transcript as a note.
  Rationale: one set of bounds for one idea, and the twenty-turn limit is what stops a loop of work that starts work.
  Date/Author: 2026-09-29.
- Decision: the tools take `background` (true or false). A survey defaults to background, a subagent to waiting, and the description says when to choose which.
  Rationale: a survey is always minutes; a subagent is often quick, and waiting keeps its answer in the same turn when it is.
  Date/Author: 2026-09-29.
- Decision: a subagent started in the background has the tools its parent had, minus `subagent` and minus anything outward-facing unless the parent turn had the person present, which is the same rule its woken turn follows.
  Rationale: it runs after the person may have looked away, like a woken turn, and must not be able to do more than one.
  Date/Author: 2026-09-29.

## Outcomes & Retrospective

Nothing yet.

## Context and Orientation

The agent's model calls run in "runs": a conversation of kind run (`models.AgentConversationRun`) with a job kind (`jobKind`) naming what kind of work it was. The subagent tool (`internal/agent/tools_subagent.go`) makes a run titled "Subagent: ..." with kind `subagent` and calls `Agent.Ask` with the parent's tools minus itself, waiting up to `subagentWait`. The survey (`internal/agent/survey.go`, `Agent.Survey`) asks one headless run per page in scope, several at once, and one more to combine them; its tool is `internal/agent/tools_survey.go`, offered only when the person is present, once a turn; `SurveyAgentMemory` in `internal/api/v1api/apigraph` runs it as a long query; `teanode agent survey` (`internal/cmd`) calls that query with a 16 minute client timeout.

Jobs are rows of `agent_job` (`models.AgentJob`: kind, subject, status queued/running/done/failed, attempts), claimed by workers in `internal/agent` (see `job_policy.go` for per-kind policy and where each kind is dispatched). Background shell commands are in `internal/agent/background.go`: an ending from the computer is gathered for two seconds (`backgroundWakeGather`), then `tryWakeForBackground` starts a turn in the conversation with `Surface: "background"` and a message beginning with `models.BackgroundCommandMarker`, counts it in `backgroundWakeCounts` (at most `backgroundWakesAlone`, reset by `personTookTurn`), writes a note instead when out of turns or budget, and acknowledges the ending when the turn is over.

## Plan of Work

Milestone 1. Migration (next free number) creates `agent_background_work`: `id`, `agent_id`, `conversation_id` (the one to wake; empty when started from the API or the command line), `work_kind` (`survey` or `subagent`), `title`, `request jsonb` (survey: question, scope path; subagent: prompt, allowed tool names), `is_person_present boolean`, `work_status` (queued, running, done, failed, stopped), `result text` (the report or the subagent's answer), `run_ids jsonb`, `error text`, `created_at`, `started_at`, `finished_at`, `woken_at`. `models.AgentBackgroundWork` with the fields; db methods to create, get, list by agent (newest first), mark running/finished/stopped, and mark woken. A job kind `background` whose subject is the row: its worker loads the row, runs `Agent.Survey` or a subagent run (the same code the tools use, factored so both paths share it), stores the result, run ids and status, then hands the finished row to milestone 2's waker. A job claimed again after a restart finds the row running and runs it again. Stopping: an in-process registry of running work by id holds a cancel function; `StopBackgroundWork` cancels it if it runs here, and marks the row stopped either way; the worker, seeing a cancelled context, stores stopped rather than failed. Work older than a day that is still queued or running is marked failed by a sweep, since its job is gone.

Milestone 2. Generalize the waker in `background.go` so a wake carries endings of either kind: a computer's command ending, or finished background work. A finished row with a conversation and `is_person_present` joins that conversation's wake (the same gathering, the same count, the same note when out of turns or budget), and the message gets a section per finished work: `[background work]` marker (a new `models.BackgroundWorkMarker`), what it was (survey of scope, or subagent titled ...), how it ended, and the result fenced as data (bounded; the full result is readable with the tool's `read`). The row is marked woken when the turn is over. Rows without a conversation (from the API) wake nothing.

Milestone 3. The survey tool gains `background` (default true) and the subagent tool `background` (default false); in the background they create the row and enqueue the job, and return `{backgroundWorkId, note: "you will be woken when it finishes"}`. A tool `background_work` (actions list, read, stop) reaches the person's background work, the way the shell tool reads and stops commands; offered where survey or subagent is. GraphQL: `StartAgentSurvey(question, scopePath): AgentBackgroundWorkView` (a mutation that queues and returns at once), `ListAgentBackgroundWork`, `GetAgentBackgroundWork(id)`, `StopAgentBackgroundWork(id)`. `SurveyAgentMemory` stays for compatibility but the CLI stops using it: `teanode agent survey` starts one and polls `GetAgentBackgroundWork` every few seconds until it finishes, printing the report (`--no-wait` prints the id and returns). `teanode agent background list|show|stop`. Client documents in `internal/client` with the schema test.

Milestone 4. The dashboard lists background work where runs are listed (the agent page's runs area): each with what it is, status, when, and for finished ones the result opened in the same reader runs use; running ones have Stop. Docs: `docs/subsystems/the-ask-loop.md` (or where subagents are described), `docs/subsystems/memory.md` for the survey, `docs/reference/command-line.md`, and a decision record under `docs/decisions/` naming how it extends the background command decision. Deploy, then in the drawer start a survey in the background with an invented question about an area of the notes, see the immediate answer, the woken turn with the report, and `teanode agent background list` in between.

## Concrete Steps

From the repository root; database tests need Docker.

    go test -mod=vendor ./internal/agent/ -run 'Background|Survey|Subagent'
    go test -mod=vendor ./internal/db/ ./internal/api/v1api/apigraph/ ./internal/client/ ./internal/cmd/...
    cd web && npx tsc --noEmit -p . && npx vitest run
    set -o pipefail; make lint-ci

## Validation and Acceptance

Milestone 1 when a queued row runs, stores its result and run ids, survives a restart of the worker in a test by running again, and can be stopped. Milestone 2 when a finished row wakes its conversation once, with the marker and the fenced result, counts toward the twenty, and writes a note instead when out of turns. Milestone 3 when the agent's survey returns at once in a turn and the conversation is woken with the report, the subagent in the background does the same, and `teanode agent survey` prints a report without a long request. Milestone 4 when the dashboard shows running and finished work and Stop stops one.

## Idempotence and Recovery

The migration only creates a table; its reverse drops it. A job run twice after a restart runs the work twice and wakes once per finish, at worst twice, which the background command decision already accepts. A row whose job vanished is failed by the sweep, never left running.

## Interfaces and Dependencies

    // internal/models
    type AgentBackgroundWork struct { ID, AgentID, ConversationID, WorkKind, Title string; Request json.RawMessage; IsPersonPresent bool; WorkStatus string; Result string; RunIDs []string; Error string; CreatedAt time.Time; StartedAt, FinishedAt, WokenAt *time.Time }
    const AgentJobBackground AgentJobKind = "background"
    const BackgroundWorkMarker = "[background work]"

No new libraries.
