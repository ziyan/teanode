# Unattended actions and adjustable limits: the person chooses what the agent may do alone, and the operator sets every cap

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds.


## Purpose / Big Picture

A run with nobody present (a schedule, a goal, a night, a message to the agent by mail) now reaches the person's devices, but any call that needs the person's word is still refused there, because nobody can give it: sending mail as them, deleting something for good, sharing something, a command the judgement says asks, a tool on their own "ask me first" list. For a personal agent that keeps at things in the background this is the wall it hits most. After this plan the person chooses, on the agent page's settings, which of those kinds of action the agent may take when they are not there. Each is off until they turn it on.

Separately, the numbers that bound the agent's work (forty rounds a turn, forty-eight goal turns a day, twenty-four turns alone before a goal stops, twenty goals at once, twenty wakes of a conversation by finished work, twenty rounds a subagent) are constants in the code. After this plan each is a server limit the operator sets on the server's agent settings page, beside the round caps already there, and the turn's default rises from 40 rounds to 150.

To see it working: on the agent page's settings, under "When you are not there", turn on "Send messages as you". A schedule that says "email me the weather at 7" then sends the mail instead of saying what it would have sent. On the server's agent settings, set "Rounds per turn" to 200 and a long turn runs past 150.


## Progress

- [x] (2026-10-06) Surveyed the confirmation gate (`internal/agent/ask.go`, `tools.NeedsConfirmation`, `command_judge.go`), the tools that check `call.Confirmed` themselves, the agent settings path (model, migration, database, `UpdateAgent`, client, `teanode agent settings`, the agent page), and every cap.
- [x] (2026-10-06) Milestone 1: unattended allowances, end to end: migration 0151, the gate, the API, `teanode agent settings set unattended=`, `agent_profile`, and the agent page.
- [x] (2026-10-06) Milestone 2: the caps as server limits, end to end, on the server's agent settings page and in `teanode settings`.
- [x] (2026-10-06) Milestone 3: tests and docs; checked in Chrome at 1400 and 390 pixels, light and dark; deployed. The maintainer's server had 40 rounds a turn saved, which the new default does not change; it was raised to 150 with `teanode settings set agent 'limits:={"maxRoundsPerAsk":150}'`.


## Surprises & Discoveries

- `teanode settings show agent` asks for the limits by name, so a new limit is invisible there until the client's selection names it.
- A saved configuration keeps the round cap it saved: raising the default changes nothing on a server whose operator once pressed Save on the limits.


## Decision Log

- Decision: four kinds, matching why a call needs the person: `outward` (it speaks for them: sends mail, posts a message), `destructive` (it cannot be undone: deletes for good, overwrites), `granting` (it gives somebody access), and `listed` (a tool on their own or the operator's "ask me first" list). The command judgement's `outward` and `destructive` are the same kinds. A call that needs confirmation for several reasons runs unattended only when every one is allowed.
  Rationale: these are the reasons the code already distinguishes; a per-tool list would duplicate the confirm list and grow with every tool.
  Date/Author: 2026-10-06.
- Decision: allowed kinds apply wherever nobody can confirm: a run with nobody present, a background subagent, and the surfaces that cannot show a card (mail, schedule). They do not loosen a read-only run: the runs that read strangers' mail (sorting, drafting, research) stay read-only, so an allowance never lets a stranger's message steer an action.
  Date/Author: 2026-10-06.
- Decision: the agent may read the setting but not change it from a run with nobody present, whatever is allowed; from a conversation it changes it only through a confirmed call.
  Rationale: otherwise an unattended run that was allowed one kind could grant itself the rest.
  Date/Author: 2026-10-06.
- Decision: the caps are operator limits (`agent.limits` in the configuration), zero meaning the default, shown on the server's agent settings page; not per person.
  Rationale: they bound what the server spends, which is the operator's to decide, and the round caps already live there.
  Date/Author: 2026-10-06.


- Review, 2026-10-07: `outward` covered money and the judge's `destructive` covered sharing, so ticking "speak for you" let a goal place an order; money is its own kind now and the judge answers `money` and `granting`. The operator's confirm list had become allowable as `listed`; it is its own reason no person can allow. The allowance reached read-only runs through a read tool on a confirm list; restricted runs never use it now. A background subagent could widen the setting with a call the allowance let through; the setting changes only on a card the person answered. The turn prompts still told the model nothing needing confirmation could be done; they now list what is allowed. Subagents of unattended turns and turns woken in a goal's conversation waited on cards nobody would see; they use the allowance.

## Outcomes & Retrospective

(To be written when the work is done.)


## Context and Orientation

- `internal/agent/ask.go`, around the call to `NeedsConfirmation`: the gate that refuses with `needs_confirmation: nobody is present`.
- `internal/agent/tools/tool.go`: `NeedsConfirmation` and the risk classes.
- `internal/agent/command_judge.go`: `judgedToAsk`, the fast model's verdict on a skill or MCP call.
- `internal/models/agent.go`, `internal/db/database_agent.go`, `internal/api/v1api/apigraph/agent.go`, `internal/client/agent.go`, `internal/cmd/agent.go`, `web/src/pages/agent.tsx`: the agent's settings, layer by layer.
- `internal/config/agent.go` (`AgentLimits`), `web/src/pages/settings/agentSettings.tsx`, `docs/configuration.md`: the operator's limits.
- The caps: `goalTurnsPerDay`, `goalTurnsAlone` (`internal/agent/goal.go`), `goalsInProgressMost` (`goal_background.go`), the woken-turn cap (`background.go`), `subagentRounds` (`tools_subagent.go`), `MaxRoundsPerAsk`.


## Plan of Work

Milestone 1. Migration 0151 adds `agent.unattended_allowed_risks` (jsonb, `[]`). `models.Agent.UnattendedAllowedRisks` with validation against the four kinds. `tools.ConfirmationReasons` gives the kinds a call needs confirmation for; `NeedsConfirmation` becomes "any reason". `judgedToAsk` returns the judged kind. In `ask.go`, when nobody can confirm, a call whose reasons are all allowed runs with `call.Confirmed` set and a log line; otherwise it is refused with a message naming the setting. `UpdateAgent` takes `unattendedAllowedRisks`; the client, `teanode agent settings set unattended=`, and the `agent_profile` tool (read, and change with confirmation, never unattended) carry it; the agent page gets a "When you are not there" form under "Ask me first".

Milestone 2. `AgentLimits` gains `GoalTurnsPerDay`, `GoalTurnsAlone`, `GoalsInProgress`, `BackgroundWakesAlone`, `MaxRoundsPerSubagent`, each zero for the default, with resolvers; the code reads them instead of constants. `MaxRoundsPerAsk` defaults to 150. The server's agent settings page and `docs/configuration.md` gain the fields.

Milestone 3. Tests for the gate (allowed, partly allowed, refused), the setting's validation and round trip, the limits' resolution; docs; a run on the server.


## Validation and Acceptance

`make test` and `make lint-ci` pass. On the server, with `outward` allowed, a schedule that sends mail sends it; with it off, the schedule's transcript says what it would have sent. The server settings page shows and saves the new limits.


## Idempotence and Recovery

The migration only adds a column with a default. Every setting is off or at its old value until changed, except the turn's round cap, which rises to 150; an operator who set it explicitly keeps their value.
