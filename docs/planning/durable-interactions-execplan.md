# Questions and approvals that wait for the person

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds.

## Purpose / Big Picture

The agent stops to ask the person two kinds of thing: a question (the `ask_user` tool, drawn in the chat drawer as a card with choices and a line to type in) and an approval (a card with Approve and Decline, raised before a tool whose risk class needs the person's word, such as sending mail). Today both live only inside the turn that raised them. The turn waits ten minutes; if nobody answers, the question fails and the approval counts as refused. A card is kept nowhere but in the turn's memory, so once the turn is over, reloading the page shows the call as a finished tool line with nothing to press.

People are not always watching. After this change a question or an approval waits for as long as it takes. The turn waits a few minutes, as now, for somebody who is looking; after that it ends, telling the model the card is still open. The card stays in the conversation, across a reload and on another device. When the person answers or approves later, a new turn starts in that conversation carrying the answer, and an approved call runs without asking again.

To see it working: ask the agent something that needs approval, such as sending a test mail, and close the tab. Five minutes later reopen the dashboard: the approval card is still in the chat. Approve it: a turn starts and the mail is sent. The same with a question: the agent asks, you answer an hour later, and it carries on with your answer.

## Progress

- [x] (2026-09-24) Wrote this plan.
- [x] (2026-09-24) Milestone 1: interactions are stored, and the turn leaves them open instead of failing.
- [x] (2026-09-24) Milestone 2: a late answer or approval resumes the work in a new turn.
- [x] (2026-09-24) Milestone 3: the drawer draws open cards from the store, and draws the resuming messages.

## Surprises & Discoveries

- Observation: `askuser` cannot import `internal/agent` (the agent imports every tool package), so `ErrLeftOpen` is defined in `internal/agent/tools` and `agent.ErrLeftOpen` refers to it.
- Observation: the drawer reads a conversation in one GraphQL document, and an operator may read another person's conversation; `ListAgentInteractions` answers an empty list for a conversation that is not the caller's rather than failing the whole read.

## Decision Log

- Decision: a late answer starts a new turn rather than keeping the old one alive.
  Rationale: a turn is a job with a deadline, holding a model context, a place in the conversation's queue and a worker slot. Keeping it for hours would block the person's other messages behind it and lose it on every deploy. The conversation's history already holds the question and the model's reason for asking, so a new turn with the answer has everything the old one had.
  Date/Author: 2026-09-24, agent.

- Decision: an approval given late lets the new turn make exactly the approved call once, matched by tool name and arguments; any other call, or the same tool with other arguments, asks again.
  Rationale: the person approved one thing they read on the card. The model redoing the call is the only way to run it inside a turn with the right tools and context, and matching the arguments keeps the approval from covering something else.
  Date/Author: 2026-09-24, agent.

- Decision: a subagent's approvals (raised in its parent's conversation through `confirmVia`) keep the old behavior and are refused when nobody answers in time.
  Rationale: resuming would have to recreate the subagent. Rare enough to leave for later.
  Date/Author: 2026-09-24, agent.

## Context and Orientation

A turn is an `AskRun` (`internal/agent/ask.go`), started by `Agent.Ask` with `AskSettings`. `AskRun.Ask(ctx, callId, question, choices)` emits an event of kind `question`, waits on a channel for `AskRun.Answer`, and gives up after `confirmationWait` (ten minutes). `AskRun.confirm(ctx, tool, call)` does the same for approvals with an event of kind `confirmation` and `AskRun.Resolve`. The GraphQL mutations `AnswerAgentQuestion(runId, callId, answer)` and `ResolveAgentConfirmation(runId, callId, approve)` (`internal/api/v1api/apigraph/agent_memory.go` and `agent_ask.go`) reach the run through `commandAgentRun`, which finds the run on this instance or forwards the command to the instance running it. A run that has ended is not found.

The drawer (`web/src/components/agentDrawer.tsx`) turns events into lines: `question` into a `QuestionCard`, `confirmation` into a confirmation card. A conversation read back from the database (`linesOf`) shows each tool call as a tool line; nothing there is a card.

Messages that begin with a marker in brackets are turns the agent takes on its own (`models.OwnTurnMarkers` in `internal/models/insight.go`); the drawer draws them as a line rather than as the person's bubble (`checkInOriginOf`).

## Plan of Work

Milestone 1. Add table `agent_interaction` (migration 0113) with `id`, `agent_id` (cascade), `conversation_id`, `run_id`, `call_id`, `interaction_kind` (`question` or `approval`), `tool_name`, `tool_arguments` (the call's JSON), `interaction_text` (the question, or the card's preview line), `interaction_choices`, `tool_risk`, `created_at`, `resolved_at`, `interaction_answer` (the answer, `approved` or `declined`, or `stopped`), with model `models.AgentInteraction` and methods in `internal/db/database_interaction.go`: create, get by call, list open for a conversation, and `ClaimAgentInteraction`, a conditional update that resolves a row only if it is still open and says whether it did, so that of the live turn and a late resume exactly one acts on an answer. `AskRun.Ask` and `AskRun.confirm` write the row when they raise the card, wait `interactionLiveWait` (five minutes), claim it when an answer arrives, and on timeout return `ErrLeftOpen` and leave it open; a stopped turn claims it as `stopped`. `ask_user` answers `ErrLeftOpen` with a result telling the model the question stays open and to end its turn; the approval path answers with an `awaiting_approval` error saying the same.

Milestone 2. The two mutations look the card up by call. If it is resolved, they refuse ("already answered"). Otherwise they try the live turn as now; if that did not take it, they claim the row and start a turn in the conversation, as the person, on the drawer surface, with a message beginning `[answering]` (the question, then the answer) or `[approved]` / `[declined]` (the preview line; for an approval, the tool and arguments to call). An approval sets `AskSettings.PreApproved`, a set of tool name and canonical arguments that `NeedsConfirmation` skips once. "Chat about it" claims the row with no turn. These messages are the person's word and are not own-turn markers.

Milestone 3. `ListAgentInteractions(conversationId)` returns the open cards; the drawer reads them with the conversation and draws each as its card, keyed as the live event would be, so a card seen live and read back is one card. A message beginning `[answering]` is drawn as the person's bubble with the question above it; `[approved]` and `[declined]` as a line.

## Concrete Steps

From the repository root, with a test database (`docs/reference/local-development.md`):

    TEANODE_TEST_DATABASE_HOST=<host> go test ./internal/agent/... ./internal/db/... ./internal/api/...
    make lint-ci

## Validation and Acceptance

A test with a fake model raises a question, lets the live wait pass (the wait is a variable a test shortens), sees the turn end with the card open, answers through the mutation, and sees a new turn whose message carries the answer. The same for an approval of a tool that needs one: after the late approval the new turn's call runs without a second card; a call with other arguments raises a card again. In the browser, a card survives a reload.

## Idempotence and Recovery

The migration is additive with a reverse. A row claimed twice is resolved once. A deploy mid-wait leaves the row open, which is what a timeout does, so the card survives a deploy too.

## Interfaces and Dependencies

In `internal/models`: `AgentInteraction` with `InteractionKind` (`question`, `approval`). In `internal/db`: `CreateAgentInteraction`, `GetAgentInteractionByCall(agentId, callId)`, `ListOpenAgentInteractions(conversationId)`, `ClaimAgentInteraction(id, answer) (bool, error)`. In `internal/agent`: `ErrLeftOpen`, `AskSettings.PreApproved`, `Agent.ResumeInteraction`. No new libraries.
