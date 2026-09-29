# Background work: surveys and subagents that run on and wake the conversation that started them

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds.

## Purpose / Big Picture

A survey takes minutes, and a subagent going through a repository can take ten. Today both run inside the turn that asked for them: the person watches the turn sit on one tool call, the turn cannot do anything else meanwhile, and a request from the command line or the API holds one HTTP request open for the whole time, which a proxy with a short timeout cuts off. Shell commands had the same problem and solved it: a command can run in the background, the turn ends, and when the command ends the conversation that started it is woken with the result (`docs/decisions/20260923-a-background-command-wakes-the-conversation-that-started-it.md`).

After this plan the agent can start a survey or a subagent in the background. The tool call returns at once with an id; the turn goes on or ends; when the work finishes, the conversation is woken with a turn that begins `[background work]` and carries the result, and the agent tells the person what came of it. The dashboard, the command line and the agent can all list background work, read one, and stop one. `teanode agent survey` starts a survey and waits for it by asking for its result, not by holding one request open, so it works behind any proxy.

To see it working: in the drawer ask "survey my notes on the garden in the background and tell me when it's done"; the agent answers at once that it has started; a minute or two later a new turn appears with the report. `teanode agent background list` shows it while it runs.

## Progress

- [x] (2026-09-29) Read how background shell commands wake a conversation (`internal/agent/background.go`) and how jobs run; wrote this plan.
- [x] (2026-09-29) Milestone 1: the record and the job: `agent_background_work` (migration 0121), the job kind `background`, stopping (here through a cancel, elsewhere through the row), the result kept, and a sweep that fails work whose job was lost.
- [x] (2026-09-29) Milestone 2: waking the conversation when it finishes, through the same waker and the same count of twenty as background commands; a note instead when out of turns or budget; stopped work wakes nothing.
- [x] (2026-09-29) Milestone 3: the tools, the API and the command line: survey and subagent with `background`, the `background_work` tool, `StartAgentSurvey`, `ListAgentBackgroundWork`, `GetAgentBackgroundWork` and `StopAgentBackgroundWork` with their client documents, `teanode agent survey` by start and ask, and `teanode agent background list|show|stop`.
- [x] (2026-09-29) Milestone 4, the dashboard and docs: the agent page's Activity tab lists background work above the runs, with Stop while it is queued or running and Open for the run that holds a finished one's result; the drawer draws a `[background work]` turn as a quiet line; `the-ask-loop.md`, `jobs-and-schedules.md`, `memory.md`, `devices.md`, `command-line.md` and `docs/decisions/20260929-background-work-wakes-the-conversation-that-started-it.md`.
- [x] (2026-09-29) Review fixes: a wake claims its row so two instances never both wake a conversation; the count of woken turns moved to the conversation's row (migration 0122 for both); a background subagent is held to the permissions of the turn that started it; `teanode agent survey` keeps waiting through passing failures, gives up after thirty minutes, and names the id on every exit; the dashboard's Open shows the result text itself, and a list that cannot be read says so in a toast.
- [ ] Milestone 4, the rest: deploy, then in the drawer start a survey in the background with an invented question, see the immediate answer, the woken turn with the report, and `teanode agent background list` in between. Left for the session that deploys.

## Surprises & Discoveries

- The general stale-claim rule puts back any running job claimed more than fifteen minutes ago, except the night and the ingest. A survey may take a quarter of an hour, so background work would have been claimed a second time beside itself; it gets a bound of its own, released by kind the way the night and the ingest are.
  Evidence: `ReleaseStaleAgentJobs` in `internal/db/database_agent.go`, and `jobTimeout` defaulting to ten minutes.
- Review found the waker's guards were all in one instance's memory: the in-flight set kept the job's wake and the sweep apart only on the same instance, so another instance's sweep a minute later woke the conversation a second time while the first turn still ran; and `backgroundWakeCounts` was per instance, so a chain of woken turns spread over instances never reached twenty.
  Evidence: `wakeForBackgroundWork` and `tryWakeForBackground` in `internal/agent/background.go` before commit fbfafb35.
- Nothing narrows a turn's operations below the person's own permissions today: every `agentOperations` is built from `EffectivePermissions` of the user, and `Execute` resolves the principal again at each call. A background subagent that made its operations afresh would still have reached any permission granted between its start and its run.
  Evidence: `agentOperations` in `internal/api/v1api/apigraph/agent_ask.go`, `operationsFor` in `agent_memory.go`, `mcpPerson` in `agent_mcp.go`.
- The client reported a proxy's 502 page as an untyped error, so the command line could not tell a server restarting from a real refusal.
  Evidence: the two GraphQL paths in `internal/client/client.go`.
 goes back in the queue without a mark (`outcomeForJob`), which for background work would run it again forever. The work gets a bound (`backgroundWorkLongest`, twenty minutes) a little inside the job's, so reaching it is recorded as failed on the row and the job ends done.

## Decision Log

- Decision: the row's columns are named for what they hold: `work_request` (typed, `models.AgentBackgroundWorkRequest`) rather than `request`, `result_text` rather than `result`, `error_message` rather than `error`.
  Rationale: the project's naming rule for keys and fields; the same names run from the column to the GraphQL view.
  Date/Author: 2026-09-29.
- Decision: a stop is the row first. `StopBackgroundWork` marks it stopped in the caller's transaction and cancels the work after commit when it runs on this instance; running work also reads its row every five seconds and cancels itself when it says stopped, which is how a stop made on another instance reaches it. Finishing never overwrites a stop.
  Rationale: the in-process registry cannot reach another instance, and a stop that only sometimes works is worse than one that takes five seconds.
  Date/Author: 2026-09-29.
- Decision: a subagent in the background is not headless but puts no card to anybody (`isUnattended` on its settings): a call that needs the person's word is refused and it says what it would have done. Its tools are fixed when it is started and kept in the row, so a run after a restart has the same ones.
  Rationale: the card of a waiting subagent is shown in the parent turn, which has ended; a card raised in the subagent's own run would sit in a conversation nobody reads. The woken turn can raise the card instead.
  Date/Author: 2026-09-29.

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
- Decision: finished work joins the waker in `background.go` as a second kind of ending (`backgroundWake.works`), under the same in-flight set (keyed `work:<id>`), the same gathering, retries and `backgroundWakeCounts`. A wake of work alone begins with `[background work]`; one with commands begins with `[background command]`, and every piece of work in either is marked `[background work]` again. The result is fenced and cut at 12,000 characters; a failure's message is fenced too.
  Rationale: one counter is what makes a chain of work that starts commands that start work stop at twenty.
  Date/Author: 2026-09-29.
- Decision: a wake lost to a restart is recovered by the minute's sweep, which wakes done or failed work with a conversation, the person present and no `woken_at`, finished between one minute and one hour ago. `woken_at` is written when the woken turn is over (or the note written, or the conversation found gone).
  Rationale: a computer says an ending again until it is acknowledged; the row is what says it again here. Finished longer than an hour ago is no longer news.
  Date/Author: 2026-09-29.
- Decision: a turn with nobody present starts no background work; the tools refuse and say to wait instead. So work a tool starts always has the person present, and the plan's "minus anything outward-facing unless the person was present" never has anything to remove; the background subagent keeps the parent's tools less `subagent`, `survey` and `background_work`, and its cards are refused as above.
  Rationale: the background command decision's rule, that a turn with nobody present never leaves a command running; it has ended by the time the work finishes and nobody reads it.
  Date/Author: 2026-09-29.
- Decision: one survey a turn whether it waits or not; a second is refused with `errSurveyedThisTurn`, reworded to cover both. A woken turn is a turn of its own and may start one, which the twenty bound.
  Rationale: starting it in the background does not change what it costs, one call a page in scope, and a second survey in one turn is almost always the same question again.
  Date/Author: 2026-09-29.
- Decision: `background_work` is built by the agent, like `survey` and `subagent`, and offered where they are (a turn with somebody present, not in a subagent, the `subagents` feature on), in the round from the start. It is not registered in the catalog, so `TestTheCatalogStaysShort` is unchanged.
  Rationale: stopping has to reach the cancel of work running on this instance, which only the agent holds; and the woken message names the tool, which a deferred tool would answer with "not loaded".
  Date/Author: 2026-09-29.
- Decision: `StartAgentSurvey` refuses at once when nothing can run a survey (`Agent.CanSurvey`), rather than queueing work that fails. The client's `SurveyAgentMemory` call and its document are removed, since nothing sends them; the query stays on the server.
  Rationale: a client function nobody calls is a second way to do one thing; the schema test covers the documents that are sent.
  Date/Author: 2026-09-29.
- Decision: on the dashboard each piece of work is a `SettingsRow` with one text action: Stop (`link danger`) while queued or running, Open once done, which opens the run holding the result in the drawer as the activity table opens a run (a subagent's one run; a survey's last, the one that combined the parts). Stop asks nothing first and says how it went in a toast, as a background command's Stop does. The card is absent when there is no work, like the background commands card.
  Rationale: `docs/coding/frontend-design.md` (one action is a word; toasts for success and failure), and the runs' reader is the transcript the drawer already opens.
  Date/Author: 2026-09-29.
- Decision (review, superseding the Open above): Open, on done or failed work, reads the work and shows its `resultText` in a wide `ConfirmDialog` through the `Markdown` reader the transcript uses, or the error for failed work, with the runs it made listed under it as links that open each in the drawer. A list that cannot be read says so once in a toast, de-duplicated as `backgroundCommands.tsx` does.
  Rationale: which run holds a survey's report is a guess (the last one happened to be the combining run), and the result is on the row already; the runs stay a click away for somebody who wants the working.
  Date/Author: 2026-09-29.
- Decision: a wake for finished work claims the row first with a conditional update (`agent_background_work.wake_claimed_at`, set only where `woken_at` is null and the claim is null or older than thirty minutes), from the job's own wake and from the sweep; only the claimer wakes, the sweep lists only unclaimed or expired rows, and a wake given up releases its claim.
  Rationale: the database is the one thing every instance shares; thirty minutes is longer than a woken turn takes, so the claimer is never raced while its turn runs, and short enough that a server that went down mid-wake delays the wake, not loses it inside the sweep's hour.
  Date/Author: 2026-09-29.
- Decision: the count of woken turns is `agent_conversation.background_wake_count`, added to by the waker once `Ask` has started the woken turn (commands and work alike, as before), and reset in `keepPersonTurn`, in the transaction that stores what the person wrote (a turn not headless and not of surface `background`, and steered messages too). The in-memory map and `personTookTurn` are gone. `UpdateAgentConversation` does not write the column, so a change to the conversation never loses a count.
  Rationale: the bound has to hold whichever instance each wake of a chain lands on; resetting with the message itself means a wake on any instance reads the two together.
  Date/Author: 2026-09-29.
- Decision: a background subagent keeps the starting turn's `Operations.Permissions()` in `work_request.startingTurnPermissions`, and when it runs its operations, made afresh for the person, are narrowed to it (`narrowOperations`): `agentOperations.NarrowedTo` intersects the set it offers tools by, and holds every `Execute` to the person's permissions at the call intersected with the limit (`EffectivePermissions.Within`). A row without the set, from before this, is run unnarrowed.
  Rationale: made afresh, the operations follow a permission taken away since; narrowed, they never reach one granted since, or anything the starting turn could not.
  Date/Author: 2026-09-29.
- Decision: `teanode agent survey` retries a read that fails with a `client.ConnectionError` or a `client.StatusError` of 5xx, 429 or 408 (`client.IsTransient`), doubling the wait up to a minute, says each failure on standard error, stops waiting after thirty minutes, and names the id and `teanode agent background show <id>` on every exit, including success.
  Rationale: the survey goes on on the server whatever happens to the command; a server restarting behind its proxy is the ordinary failure and must not end the wait, and whoever is left without the report needs the id to read it later.
  Date/Author: 2026-09-29.

## Outcomes & Retrospective

Milestones 1 to 4 are in code, tested and documented; the end-to-end check on a deployed server is still to do. What shipped matches the plan's shape, with three narrowings recorded above: a turn with nobody present starts no background work at all (so the outward-facing rule for the subagent has nothing to act on), a background subagent shows no cards, and one survey a turn holds whether it waits or not. The one thing the plan did not foresee was the job queue's own bounds: the general stale-claim release and the requeue on a deadline both had to be kept away from background work, or a survey would have been run twice at once, or forever.

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

The migration only creates a table; its reverse drops it. A job run twice after a restart runs the work twice, which the background command decision already accepts; the conversation is woken once, by whoever claims the row's wake. Migration 0122 only adds two columns; its reverse drops them. A row whose job vanished is failed by the sweep, never left running.

## Interfaces and Dependencies

    // internal/models
    type AgentBackgroundWork struct { ID, AgentID, ConversationID, WorkKind, Title string; Request json.RawMessage; IsPersonPresent bool; WorkStatus string; Result string; RunIDs []string; Error string; CreatedAt time.Time; StartedAt, FinishedAt, WokenAt *time.Time }
    const AgentJobBackground AgentJobKind = "background"
    const BackgroundWorkMarker = "[background work]"

No new libraries.
